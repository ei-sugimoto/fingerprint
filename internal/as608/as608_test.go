package as608

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

var errEmpty = errors.New("buffer empty")

// fakePort は Port を満たすテスト用のポート。written に送信内容を溜め、
// 命令を受け取ったら response を受信バッファに積む。stale は命令より前から
// バッファに残っているバイト。
type fakePort struct {
	written  bytes.Buffer
	stale    []byte
	response []byte
}

func (p *fakePort) Write(b []byte) (int, error) {
	n, err := p.written.Write(b)
	p.stale = append(p.stale, p.response...)
	p.response = nil
	return n, err
}

func (p *fakePort) ReadByte() (byte, error) {
	if len(p.stale) == 0 {
		return 0, errEmpty
	}
	b := p.stale[0]
	p.stale = p.stale[1:]
	return b, nil
}

// ack は確認コード code とデータ data を持つ応答パケットを組み立てる。
func ack(code Status, data ...byte) []byte {
	return appendPacket(nil, DefaultAddress, PacketAck, append([]byte{byte(code)}, data...))
}

func newTestDevice(response []byte) (*Device, *fakePort) {
	port := &fakePort{response: response}
	dev := New(port)
	dev.Timeout = 10 * time.Millisecond
	return dev, port
}

func TestAppendPacketVfyPwd(t *testing.T) {
	got := appendPacket(nil, DefaultAddress, PacketCommand, []byte{cmdVfyPwd, 0, 0, 0, 0})
	want := []byte{
		0xEF, 0x01, // ヘッダ
		0xFF, 0xFF, 0xFF, 0xFF, // アドレス
		0x01,       // 命令パケット
		0x00, 0x07, // 長さ = データ 5 + チェックサム 2
		0x13, 0x00, 0x00, 0x00, 0x00, // VfyPwd + パスワード
		0x00, 0x1B, // チェックサム
	}
	if !bytes.Equal(got, want) {
		t.Errorf("appendPacket = % X, want % X", got, want)
	}
}

func TestVerifyPassword(t *testing.T) {
	dev, port := newTestDevice(ack(StatusOK))

	if err := dev.VerifyPassword(DefaultPassword); err != nil {
		t.Fatalf("VerifyPassword() = %v, want nil", err)
	}

	want := appendPacket(nil, DefaultAddress, PacketCommand, []byte{cmdVfyPwd, 0, 0, 0, 0})
	if got := port.written.Bytes(); !bytes.Equal(got, want) {
		t.Errorf("送信パケット = % X, want % X", got, want)
	}
}

func TestVerifyPasswordWrong(t *testing.T) {
	dev, _ := newTestDevice(ack(StatusWrongPassword))

	err := dev.VerifyPassword(0xDEADBEEF)
	if !errors.Is(err, StatusWrongPassword) {
		t.Fatalf("VerifyPassword() = %v, want %v", err, StatusWrongPassword)
	}
}

func TestSystemParameters(t *testing.T) {
	dev, _ := newTestDevice(ack(StatusOK,
		0x00, 0x00, // ステータスレジスタ
		0x00, 0x09, // システム識別子
		0x00, 0xC8, // 容量 200
		0x00, 0x03, // セキュリティレベル 3
		0xFF, 0xFF, 0xFF, 0xFF, // アドレス
		0x00, 0x02, // パケット長 = 32 << 2 = 128
		0x00, 0x06, // ボーレート = 9600 * 6 = 57600
	))

	para, err := dev.SystemParameters()
	if err != nil {
		t.Fatalf("SystemParameters() error = %v", err)
	}

	want := SysPara{
		SystemID:      0x0009,
		Capacity:      200,
		SecurityLevel: 3,
		Address:       DefaultAddress,
		PacketSize:    128,
		BaudRate:      57600,
	}
	if para != want {
		t.Errorf("SystemParameters() = %+v, want %+v", para, want)
	}
}

func TestTemplateCount(t *testing.T) {
	dev, _ := newTestDevice(ack(StatusOK, 0x00, 0x2A))

	n, err := dev.TemplateCount()
	if err != nil {
		t.Fatalf("TemplateCount() error = %v", err)
	}
	if n != 42 {
		t.Errorf("TemplateCount() = %d, want 42", n)
	}
}

func TestCaptureImageNoFinger(t *testing.T) {
	dev, _ := newTestDevice(ack(StatusNoFinger))

	err := dev.CaptureImage()
	if !errors.Is(err, StatusNoFinger) {
		t.Fatalf("CaptureImage() = %v, want %v", err, StatusNoFinger)
	}
}

func TestReadPacketSkipsGarbage(t *testing.T) {
	// 応答の前に紛れ込んだノイズはヘッダ同期で読み飛ばされる。
	dev, _ := newTestDevice(append([]byte{0x00, 0xEF, 0x12}, ack(StatusOK)...))

	if err := dev.VerifyPassword(DefaultPassword); err != nil {
		t.Fatalf("VerifyPassword() = %v, want nil", err)
	}
}

func TestCommandDrainsStaleBytes(t *testing.T) {
	// 命令を送る前からバッファに残っているバイトは応答と取り違えないよう捨てる。
	dev, port := newTestDevice(ack(StatusOK))
	port.stale = ack(StatusWrongPassword)

	if err := dev.VerifyPassword(DefaultPassword); err != nil {
		t.Fatalf("VerifyPassword() = %v, want nil", err)
	}
}

func TestChecksumMismatch(t *testing.T) {
	response := ack(StatusOK)
	response[len(response)-1]++ // チェックサムを壊す

	dev, _ := newTestDevice(response)

	if _, _, err := dev.Command([]byte{cmdReadSysPara}); !errors.Is(err, ErrChecksum) {
		t.Fatalf("Command() = %v, want %v", err, ErrChecksum)
	}
}

func TestTimeout(t *testing.T) {
	dev, _ := newTestDevice(nil)

	if _, _, err := dev.Command([]byte{cmdReadSysPara}); !errors.Is(err, ErrTimeout) {
		t.Fatalf("Command() = %v, want %v", err, ErrTimeout)
	}
}

func TestTruncatedPacket(t *testing.T) {
	// 長さフィールドが示すぶんだけ届かないまま途切れたパケット。
	dev, _ := newTestDevice(ack(StatusOK)[:10])

	if _, _, err := dev.Command([]byte{cmdReadSysPara}); !errors.Is(err, ErrTimeout) {
		t.Fatalf("Command() = %v, want %v", err, ErrTimeout)
	}
}

func TestEmptyPayload(t *testing.T) {
	dev, _ := newTestDevice(nil)

	if _, _, err := dev.Command(nil); !errors.Is(err, ErrPayloadRange) {
		t.Fatalf("Command() = %v, want %v", err, ErrPayloadRange)
	}
}
