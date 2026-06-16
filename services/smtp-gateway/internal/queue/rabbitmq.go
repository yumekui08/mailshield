// Package queue は EventPublisher インターフェースの RabbitMQ 実装を提供する。
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
type rabbitMQPublisher struct {
	url     string
	conn    *amqp.Connection
	channel *amqp.Channel
	mu      sync.Mutex
}

// New は RabbitMQ 接続を確立して EventPublisher を返す。
func New(url string) (*rabbitMQPublisher, error) {
	p := &rabbitMQPublisher{url: url}
	if err := p.connect(); err != nil {
		return nil, err
	}
	return p, nil
}

// connect は RabbitMQ への接続と Exchange 確認を行う。mu を保持せずに呼ぶこと。
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

	p.conn = conn
	p.channel = ch
	return nil
}

// reconnect は既存の接続を閉じて再接続する。mu を保持した状態で呼ぶこと。
func (p *rabbitMQPublisher) reconnect() error {
	if p.channel != nil {
		_ = p.channel.Close()
		p.channel = nil
	}
	if p.conn != nil {
		_ = p.conn.Close()
		p.conn = nil
	}
	return p.connect()
}

// publish は mu を保持した状態で呼ぶこと。
func (p *rabbitMQPublisher) publish(ctx context.Context, body []byte) error {
	return p.channel.PublishWithContext(ctx,
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
func (p *rabbitMQPublisher) PublishMailReceived(ctx context.Context, event *domain.MailEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("mail.received イベント JSON エンコード失敗: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.publish(ctx, body); err != nil {
		slog.Warn("RabbitMQ 発行失敗・再接続中",
			"message_id", event.MessageID,
			"error", err)
		if reconnErr := p.reconnect(); reconnErr != nil {
			return fmt.Errorf("mail.received 発行失敗 (message_id=%s): %w; 再接続失敗: %v",
				event.MessageID, err, reconnErr)
		}
		if err := p.publish(ctx, body); err != nil {
			return fmt.Errorf("mail.received 発行失敗 (message_id=%s): %w", event.MessageID, err)
		}
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
