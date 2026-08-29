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
	written   bytes.Buffer
	stale     []byte
	responses [][]byte
	// repeat は responses を使い切ったあと毎回返す応答。指待ちのように
	// 同じ確認コードが返り続ける状況を作るために使う。
	repeat []byte
}

func (p *fakePort) Write(b []byte) (int, error) {
	n, err := p.written.Write(b)
	switch {
	case len(p.responses) > 0:
		p.stale = append(p.stale, p.responses[0]...)
		p.responses = p.responses[1:]
	case p.repeat != nil:
		p.stale = append(p.stale, p.repeat...)
	}
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

// newTestDevice は命令を受けるたびに responses を順に返すデバイスを作る。
func newTestDevice(responses ...[]byte) (*Device, *fakePort) {
	port := &fakePort{responses: responses}
	dev := New(port)
	dev.Timeout = 10 * time.Millisecond
	dev.FingerPollInterval = time.Millisecond
	return dev, port
}

// cmd は期待する送信パケットを組み立てる。
func cmd(payload ...byte) []byte {
	return appendPacket(nil, DefaultAddress, PacketCommand, payload)
}

// indexTable は指定した ID が使用中になっているビットマップを作る。
func indexTable(pages ...int) []byte {
	data := make([]byte, indexTableLen)
	for _, p := range pages {
		data[p/8] |= 1 << (p % 8)
	}
	return data
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

func TestExtractFeature(t *testing.T) {
	dev, port := newTestDevice(ack(StatusOK))

	if err := dev.ExtractFeature(Buffer2); err != nil {
		t.Fatalf("ExtractFeature() = %v, want nil", err)
	}
	if got, want := port.written.Bytes(), cmd(cmdImg2Tz, 2); !bytes.Equal(got, want) {
		t.Errorf("送信パケット = % X, want % X", got, want)
	}
}

func TestExtractFeatureMessyImage(t *testing.T) {
	dev, _ := newTestDevice(ack(StatusImageTooDry))

	if err := dev.ExtractFeature(Buffer1); !errors.Is(err, StatusImageTooDry) {
		t.Fatalf("ExtractFeature() = %v, want %v", err, StatusImageTooDry)
	}
}

func TestStoreTemplate(t *testing.T) {
	dev, port := newTestDevice(ack(StatusOK))

	if err := dev.StoreTemplate(Buffer1, 300); err != nil {
		t.Fatalf("StoreTemplate() = %v, want nil", err)
	}
	if got, want := port.written.Bytes(), cmd(cmdStore, 1, 0x01, 0x2C); !bytes.Equal(got, want) {
		t.Errorf("送信パケット = % X, want % X", got, want)
	}
}

func TestSearchFound(t *testing.T) {
	dev, port := newTestDevice(ack(StatusOK, 0x00, 0x03, 0x00, 0x8E))

	m, err := dev.Search(Buffer1, 0, 300)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if want := (Match{Page: 3, Score: 142}); m != want {
		t.Errorf("Search() = %+v, want %+v", m, want)
	}
	if got, want := port.written.Bytes(), cmd(cmdSearch, 1, 0x00, 0x00, 0x01, 0x2C); !bytes.Equal(got, want) {
		t.Errorf("送信パケット = % X, want % X", got, want)
	}
}

func TestSearchNotFound(t *testing.T) {
	dev, _ := newTestDevice(ack(StatusNotFound))

	if _, err := dev.Search(Buffer1, 0, 300); !errors.Is(err, StatusNotFound) {
		t.Fatalf("Search() = %v, want %v", err, StatusNotFound)
	}
}

func TestDeleteTemplate(t *testing.T) {
	dev, port := newTestDevice(ack(StatusOK))

	if err := dev.DeleteTemplate(5, 1); err != nil {
		t.Fatalf("DeleteTemplate() = %v, want nil", err)
	}
	if got, want := port.written.Bytes(), cmd(cmdDeleteChar, 0x00, 0x05, 0x00, 0x01); !bytes.Equal(got, want) {
		t.Errorf("送信パケット = % X, want % X", got, want)
	}
}

func TestEmptyLibrary(t *testing.T) {
	dev, port := newTestDevice(ack(StatusOK))

	if err := dev.EmptyLibrary(); err != nil {
		t.Fatalf("EmptyLibrary() = %v, want nil", err)
	}
	if got, want := port.written.Bytes(), cmd(cmdEmpty); !bytes.Equal(got, want) {
		t.Errorf("送信パケット = % X, want % X", got, want)
	}
}

func TestUsedPages(t *testing.T) {
	// 容量 300 なので 256 件ぶんのテーブルを 2 回読む。
	dev, port := newTestDevice(
		ack(StatusOK, indexTable(0, 2)...),
		ack(StatusOK, indexTable(1)...),
	)

	pages, err := dev.UsedPages(300)
	if err != nil {
		t.Fatalf("UsedPages() error = %v", err)
	}
	want := []uint16{0, 2, 257}
	if len(pages) != len(want) {
		t.Fatalf("UsedPages() = %v, want %v", pages, want)
	}
	for i := range want {
		if pages[i] != want[i] {
			t.Fatalf("UsedPages() = %v, want %v", pages, want)
		}
	}

	wantTx := append(cmd(cmdReadIndexTable, 0), cmd(cmdReadIndexTable, 1)...)
	if got := port.written.Bytes(); !bytes.Equal(got, wantTx) {
		t.Errorf("送信パケット = % X, want % X", got, wantTx)
	}
}

func TestUsedPagesIgnoresPagesBeyondCapacity(t *testing.T) {
	// テーブルには立っているが容量外の ID は返さない。
	dev, _ := newTestDevice(ack(StatusOK, indexTable(0, 200)...))

	pages, err := dev.UsedPages(100)
	if err != nil {
		t.Fatalf("UsedPages() error = %v", err)
	}
	if len(pages) != 1 || pages[0] != 0 {
		t.Errorf("UsedPages() = %v, want [0]", pages)
	}
}

func TestEnroll(t *testing.T) {
	dev, port := newTestDevice(
		ack(StatusOK),       // GetImage: 1 回目の指
		ack(StatusOK),       // Img2Tz(1)
		ack(StatusNoFinger), // GetImage: 指が離れた
		ack(StatusOK),       // GetImage: 2 回目の指
		ack(StatusOK),       // Img2Tz(2)
		ack(StatusOK),       // RegModel
		ack(StatusOK),       // Store
	)

	var steps []EnrollStep
	err := dev.Enroll(7, time.Second, func(s EnrollStep) { steps = append(steps, s) })
	if err != nil {
		t.Fatalf("Enroll() = %v, want nil", err)
	}

	wantTx := bytes.Join([][]byte{
		cmd(cmdGetImage),
		cmd(cmdImg2Tz, 1),
		cmd(cmdGetImage),
		cmd(cmdGetImage),
		cmd(cmdImg2Tz, 2),
		cmd(cmdRegModel),
		cmd(cmdStore, 1, 0x00, 0x07),
	}, nil)
	if got := port.written.Bytes(); !bytes.Equal(got, wantTx) {
		t.Errorf("送信パケット = % X, want % X", got, wantTx)
	}

	wantSteps := []EnrollStep{StepPlaceFinger, StepRemoveFinger, StepPlaceAgain, StepMerge, StepStore}
	if len(steps) != len(wantSteps) {
		t.Fatalf("notify の呼び出し = %v, want %v", steps, wantSteps)
	}
	for i := range wantSteps {
		if steps[i] != wantSteps[i] {
			t.Fatalf("notify の呼び出し = %v, want %v", steps, wantSteps)
		}
	}
}

func TestEnrollDifferentFinger(t *testing.T) {
	dev, _ := newTestDevice(
		ack(StatusOK),          // GetImage
		ack(StatusOK),          // Img2Tz(1)
		ack(StatusNoFinger),    // 指が離れた
		ack(StatusOK),          // GetImage
		ack(StatusOK),          // Img2Tz(2)
		ack(StatusMergeFailed), // RegModel: 2 回が別の指
	)

	err := dev.Enroll(0, time.Second, nil)
	if !errors.Is(err, StatusMergeFailed) {
		t.Fatalf("Enroll() = %v, want %v", err, StatusMergeFailed)
	}
}

func TestEnrollWaitsForFinger(t *testing.T) {
	dev, _ := newTestDevice(
		ack(StatusNoFinger), // まだ置かれていない
		ack(StatusNoFinger),
		ack(StatusOK),       // 置かれた
		ack(StatusOK),       // Img2Tz(1)
		ack(StatusNoFinger), // 離れた
		ack(StatusOK),       // GetImage
		ack(StatusOK),       // Img2Tz(2)
		ack(StatusOK),       // RegModel
		ack(StatusOK),       // Store
	)

	if err := dev.Enroll(0, time.Second, nil); err != nil {
		t.Fatalf("Enroll() = %v, want nil", err)
	}
}

func TestWaitForFingerTimeout(t *testing.T) {
	dev, port := newTestDevice()
	port.repeat = ack(StatusNoFinger) // 指が置かれないまま

	if err := dev.WaitForFinger(20 * time.Millisecond); !errors.Is(err, ErrFingerTimeout) {
		t.Fatalf("WaitForFinger() = %v, want %v", err, ErrFingerTimeout)
	}
}

func TestWaitForRemovalTimeout(t *testing.T) {
	dev, port := newTestDevice()
	port.repeat = ack(StatusOK) // 指が乗ったまま

	if err := dev.WaitForRemoval(20 * time.Millisecond); !errors.Is(err, ErrFingerTimeout) {
		t.Fatalf("WaitForRemoval() = %v, want %v", err, ErrFingerTimeout)
	}
}

func TestWaitForRemovalToleratesCaptureFailure(t *testing.T) {
	// 指を離す途中は取り込みに失敗しやすいので、それでは打ち切らない。
	dev, _ := newTestDevice(
		ack(StatusCaptureFailed),
		ack(StatusNoFinger),
	)

	if err := dev.WaitForRemoval(time.Second); err != nil {
		t.Fatalf("WaitForRemoval() = %v, want nil", err)
	}
}

func TestEnrollFingerTimeout(t *testing.T) {
	dev, port := newTestDevice()
	port.repeat = ack(StatusNoFinger)

	if err := dev.Enroll(0, 20*time.Millisecond, nil); !errors.Is(err, ErrFingerTimeout) {
		t.Fatalf("Enroll() = %v, want %v", err, ErrFingerTimeout)
	}
}

func TestIdentify(t *testing.T) {
	dev, port := newTestDevice(
		ack(StatusOK),                         // GetImage
		ack(StatusOK),                         // Img2Tz(1)
		ack(StatusOK, 0x00, 0x02, 0x00, 0x64), // Search
	)

	m, err := dev.Identify(300)
	if err != nil {
		t.Fatalf("Identify() error = %v", err)
	}
	if want := (Match{Page: 2, Score: 100}); m != want {
		t.Errorf("Identify() = %+v, want %+v", m, want)
	}

	wantTx := bytes.Join([][]byte{
		cmd(cmdGetImage),
		cmd(cmdImg2Tz, 1),
		cmd(cmdSearch, 1, 0x00, 0x00, 0x01, 0x2C),
	}, nil)
	if got := port.written.Bytes(); !bytes.Equal(got, wantTx) {
		t.Errorf("送信パケット = % X, want % X", got, wantTx)
	}
}

func TestIdentifyNoFinger(t *testing.T) {
	dev, _ := newTestDevice(ack(StatusNoFinger))

	if _, err := dev.Identify(300); !errors.Is(err, StatusNoFinger) {
		t.Fatalf("Identify() = %v, want %v", err, StatusNoFinger)
	}
}
