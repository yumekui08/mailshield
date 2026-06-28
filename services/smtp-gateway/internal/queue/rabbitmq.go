package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

const exchangeMailReceived = "mail.received"

// rabbitMQPublisher は RabbitMQ を使った EventPublisher 実装である。
//
// 排他制御の方針:
//   - mu（RWMutex）は conn/channel フィールドの読み書きを保護する。
//   - reconnectMu は再接続処理そのものを直列化する（1 ゴルーチンだけが Dial する）。
//   - ブロッキングな amqp.Dial は reconnectMu のみを保持した状態で実行し、
//     mu（RWMutex）は保持しない。これにより再接続中も他ゴルーチンの read-lock が通る。
type rabbitMQPublisher struct {
	url         string
	conn        *amqp.Connection
	channel     *amqp.Channel
	mu          sync.RWMutex // conn/channel フィールドの保護
	reconnectMu sync.Mutex   // 再接続処理の直列化
}

// New は RabbitMQ 接続を確立して EventPublisher を返す。
func New(url string) (*rabbitMQPublisher, error) {
	p := &rabbitMQPublisher{url: url}
	if err := p.connect(); err != nil {
		return nil, err
	}
	return p, nil
}

// connect は RabbitMQ への接続と Exchange 確認を行う。
// mu・reconnectMu をいずれも保持せずに呼ぶこと。
func (p *rabbitMQPublisher) connect() error {
	conn, err := amqp.Dial(p.url)
	if err != nil {
		return fmt.Errorf("RabbitMQ 接続失敗: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("RabbitMQ チャネル作成失敗: %w", err)
	}

	// Exchange の存在確認（definitions.json で作成済みのため passive declare）
	if err := ch.ExchangeDeclarePassive(
		exchangeMailReceived,
		"fanout",
		true,  // durable
		false, // auto-delete
		false, // internal
		false, // no-wait
		nil,
	); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("RabbitMQ Exchange 確認失敗 (%s): %w", exchangeMailReceived, err)
	}

	p.mu.Lock()
	p.conn = conn
	p.channel = ch
	p.mu.Unlock()
	return nil
}

// reconnect は既存の接続を閉じて再接続する。
// reconnectMu を保持した状態で呼ぶこと。mu は保持してはならない。
func (p *rabbitMQPublisher) reconnect() error {
	// 古い接続をクローズ（mu を書き込みロックして取り出す）
	p.mu.Lock()
	oldCh := p.channel
	oldConn := p.conn
	p.channel = nil
	p.conn = nil
	p.mu.Unlock()

	if oldCh != nil {
		_ = oldCh.Close()
	}
	if oldConn != nil {
		_ = oldConn.Close()
	}

	// ブロッキングな Dial は mu を保持せずに実行する
	return p.connect()
}

// currentChannel は現在のチャネルを RLock で取得する。
func (p *rabbitMQPublisher) currentChannel() *amqp.Channel {
	p.mu.RLock()
	ch := p.channel
	p.mu.RUnlock()
	return ch
}

// publish は呼び出し元が mu を保持せずに呼ぶこと。
// channel のスナップショットを引数で受け取り、PublishWithContext を実行する。
func (p *rabbitMQPublisher) publish(ctx context.Context, ch *amqp.Channel, body []byte) error {
	return ch.PublishWithContext(ctx,
		exchangeMailReceived, // exchange
		"",                   // routing key (fanout では不使用)
		false,                // mandatory
		false,                // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Body:         body,
		},
	)
}

// PublishMailReceived は mail.received イベントを fanout exchange に発行する。
// 失敗した場合は1回だけ再接続してリトライする。
// ブロッキングな amqp.Dial は mu を保持しない状態で実行するため、
// 再接続中でも他の goroutine のパブリッシュ試行・Close はブロックされない。
func (p *rabbitMQPublisher) PublishMailReceived(ctx context.Context, event *domain.MailEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("mail.received イベント JSON エンコード失敗: %w", err)
	}

	// まず現在のチャネルで発行を試みる（mu は RLock のみ）
	ch := p.currentChannel()
	if ch != nil {
		if err := p.publish(ctx, ch, body); err == nil {
			return nil
		}
	}

	// 発行失敗 or チャネルなし → reconnectMu を取得して再接続
	// reconnectMu により複数 goroutine が同時に Dial しない
	p.reconnectMu.Lock()
	// ダブルチェック: 他の goroutine がすでに再接続済みの場合は skip
	ch = p.currentChannel()
	if ch == nil {
		if reconnErr := p.reconnect(); reconnErr != nil {
			p.reconnectMu.Unlock()
			return fmt.Errorf("mail.received 発行失敗 (message_id=%s): 再接続失敗: %w",
				event.MessageID, reconnErr)
		}
		ch = p.currentChannel()
	}
	p.reconnectMu.Unlock()

	slog.Warn("RabbitMQ 再接続後にリトライ",
		"message_id", event.MessageID)

	if err := p.publish(ctx, ch, body); err != nil {
		return fmt.Errorf("mail.received 発行失敗 (message_id=%s): %w", event.MessageID, err)
	}
	return nil
}

// Close は RabbitMQ チャネルと接続を閉じる。
// 一方のクローズが失敗しても、もう一方のクローズを必ず試みる。
func (p *rabbitMQPublisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var errs []error
	if p.channel != nil {
		if err := p.channel.Close(); err != nil {
			errs = append(errs, fmt.Errorf("チャネルクローズ失敗: %w", err))
		}
	}
	if p.conn != nil {
		if err := p.conn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("接続クローズ失敗: %w", err))
		}
	}
	return errors.Join(errs...)
}
