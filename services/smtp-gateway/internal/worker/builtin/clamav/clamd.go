package clamav

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"time"
)

const chunkSize = 4096

// scanResult は clamd のスキャン結果を保持する。
type scanResult struct {
	Detected  bool
	VirusName string
}

// scan は clamd の INSTREAM コマンドでデータをスキャンする。
//
// clamd プロトコル（INSTREAM コマンド）:
//  1. "INSTREAM\n" を送信
//  2. [4バイト big-endian サイズ][データ] チャンクを繰り返す
//  3. [4バイトゼロ] でストリーム終端を通知
//  4. レスポンス1行を読む: "stream: OK\n" / "stream: <virus> FOUND\n"
//
// コンテキストの期限とタイムアウトの短い方を接続のデッドラインに設定するため、
// 親の handler_timeout_seconds に達した場合もスキャンが中断される。
func scan(ctx context.Context, addr string, timeout time.Duration, data []byte) (*scanResult, error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("clamd 接続失敗 (%s): %w", addr, err)
	}
	defer conn.Close()

	// チャンク転送ごとにデッドラインを延長するローリングデッドライン。
	// 固定デッドラインでは大きな EML の転送中に期限が切れ、ストリーム終端を送れない。
	// コンテキスト期限との調整は setChunkDeadline 内で行う。
	const chunkDeadlineExtension = 10 * time.Second

	// setChunkDeadline はコンテキスト期限と chunkDeadlineExtension の短い方をデッドラインに設定する。
	setChunkDeadline := func() error {
		deadline := time.Now().Add(chunkDeadlineExtension)
		if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
			deadline = ctxDeadline
		}
		return conn.SetDeadline(deadline)
	}

	// nINSTREAM\n: clamd は n（改行終端）または z（NUL終端）プレフィックス必須
	if err := setChunkDeadline(); err != nil {
		return nil, fmt.Errorf("clamd デッドライン設定失敗: %w", err)
	}
	if _, err := conn.Write([]byte("nINSTREAM\n")); err != nil {
		return nil, fmt.Errorf("clamd コマンド送信失敗: %w", err)
	}

	// データをチャンクで送信
	for i := 0; i < len(data); i += chunkSize {
		end := i + chunkSize
		if end > len(data) {
			end = len(data)
		}
		chunk := data[i:end]

		if err := setChunkDeadline(); err != nil {
			return nil, fmt.Errorf("clamd デッドライン設定失敗: %w", err)
		}
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(chunk)))
		if _, err := conn.Write(size[:]); err != nil {
			return nil, fmt.Errorf("clamd チャンクサイズ送信失敗: %w", err)
		}
		if _, err := conn.Write(chunk); err != nil {
			return nil, fmt.Errorf("clamd チャンクデータ送信失敗: %w", err)
		}
	}

	// ストリーム終端（4バイトゼロ）
	if err := setChunkDeadline(); err != nil {
		return nil, fmt.Errorf("clamd デッドライン設定失敗: %w", err)
	}
	if _, err := conn.Write([]byte{0, 0, 0, 0}); err != nil {
		return nil, fmt.Errorf("clamd ストリーム終端送信失敗: %w", err)
	}

	// レスポンスを1行だけ読む
	// clamd は "stream: OK\n" または "stream: <virus> FOUND\n" を返して接続を閉じる
	if err := setChunkDeadline(); err != nil {
		return nil, fmt.Errorf("clamd デッドライン設定失敗: %w", err)
	}
	resp, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("clamd レスポンス読み取り失敗: %w", err)
	}

	return parseResponse(strings.TrimSpace(resp))
}

// parseResponse は clamd のレスポンス文字列を解析する。
// 形式: "stream: OK" / "stream: <virus_name> FOUND" / "stream: <error> ERROR"
func parseResponse(resp string) (*scanResult, error) {
	switch {
	case strings.HasSuffix(resp, "OK"):
		return &scanResult{Detected: false}, nil
	case strings.HasSuffix(resp, "FOUND"):
		name := strings.TrimPrefix(resp, "stream: ")
		name = strings.TrimSuffix(name, " FOUND")
		return &scanResult{Detected: true, VirusName: name}, nil
	case strings.HasSuffix(resp, "ERROR"):
		return nil, fmt.Errorf("clamd エラー: %s", resp)
	default:
		return nil, fmt.Errorf("clamd 予期しないレスポンス: %q", resp)
	}
}
