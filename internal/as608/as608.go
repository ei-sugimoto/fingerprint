// Package as608 は AS608 系の光学式指紋センサーを UART 経由で扱うドライバ。
//
// モジュールとは 0xEF01 で始まる固定長ヘッダのパケットをやり取りする。
// 工場出荷時の設定は 57600bps / 8N1、アドレス 0xFFFFFFFF、パスワード 0x00000000。
package as608

import "time"

// 工場出荷時の設定値。
const (
	// DefaultAddress は工場出荷時のモジュールアドレス。
	DefaultAddress uint32 = 0xFFFFFFFF
	// DefaultPassword は工場出荷時のハンドシェイクパスワード。
	DefaultPassword uint32 = 0x00000000
	// DefaultBaudRate は工場出荷時のボーレート。
	DefaultBaudRate uint32 = 57600
	// DefaultTimeout は 1 パケットを受信しきるまでの既定の待ち時間。
	DefaultTimeout = 1 * time.Second
	// DefaultFingerPollInterval は指の有無を見に行く既定の間隔。
	DefaultFingerPollInterval = 100 * time.Millisecond
)

// 命令コード。
const (
	cmdGetImage       byte = 0x01
	cmdImg2Tz         byte = 0x02
	cmdSearch         byte = 0x04
	cmdRegModel       byte = 0x05
	cmdStore          byte = 0x06
	cmdDeleteChar     byte = 0x0C
	cmdEmpty          byte = 0x0D
	cmdReadSysPara    byte = 0x0F
	cmdVfyPwd         byte = 0x13
	cmdTemplateNum    byte = 0x1D
	cmdReadIndexTable byte = 0x1F
)

// Buffer はセンサー内の特徴バッファ (CharBuffer1 / CharBuffer2) を指す。
// 登録では 2 回の読み取りをそれぞれ別のバッファに入れて統合する。
type Buffer uint8

const (
	Buffer1 Buffer = 1
	Buffer2 Buffer = 2
)

// indexTableLen は ReadIndexTable が 1 回で返すビットマップの長さ。
// 1 バイト 8 件で 32 バイト = 256 件ぶん。
const indexTableLen = 32

// pollInterval は受信バッファを覗きに行く間隔。57600bps では 1 バイトが
// 約 174us なので、これより短くしても取りこぼしは減らない。
const pollInterval = 100 * time.Microsecond

// Port は AS608 とつながっているシリアルポート。machine.UART が満たす。
type Port interface {
	// Write はデータを送信しきるまでブロックする。
	Write(p []byte) (n int, err error)
	// ReadByte は受信バッファから 1 バイト取り出す。バッファが空なら
	// ブロックせず error を返す。
	ReadByte() (byte, error)
}

// Device は 1 台の AS608 を表す。
type Device struct {
	port Port

	// Address は通信相手のモジュールアドレス。
	Address uint32
	// Timeout は応答パケットを 1 つ受信しきるまでの待ち時間。
	Timeout time.Duration
	// FingerPollInterval は指が置かれる／離れるのを待つ間の撮像の間隔。
	FingerPollInterval time.Duration

	tx []byte
	rx []byte
}

// New は port につながった AS608 を表す Device を返す。
// Address と Timeout は工場出荷時の設定に合わせた既定値で初期化される。
func New(port Port) *Device {
	return &Device{
		port:               port,
		Address:            DefaultAddress,
		Timeout:            DefaultTimeout,
		FingerPollInterval: DefaultFingerPollInterval,
		tx:                 make([]byte, 0, headerLen+maxPayload+checksumLen),
		rx:                 make([]byte, maxPayload),
	}
}

// Command は payload を命令パケットとして送り、応答パケットの確認コードと
// それに続くデータを返す。payload の先頭バイトは命令コード。
//
// 返すデータは Device 内部のバッファを指すため、次に Command を呼ぶまでの
// 間しか有効でない。必要なら呼び出し側でコピーすること。
func (d *Device) Command(payload []byte) (Status, []byte, error) {
	if len(payload) == 0 || len(payload) > maxPayload {
		return 0, nil, ErrPayloadRange
	}

	// 前のやり取りの取りこぼしが残っていると応答とずれるので捨てる。
	d.drain()

	d.tx = appendPacket(d.tx[:0], d.Address, PacketCommand, payload)
	if _, err := d.port.Write(d.tx); err != nil {
		return 0, nil, err
	}

	pid, data, err := d.readPacket()
	if err != nil {
		return 0, nil, err
	}
	if pid != PacketAck || len(data) == 0 {
		return 0, nil, ErrBadPacket
	}
	return Status(data[0]), data[1:], nil
}

// VerifyPassword はハンドシェイクを兼ねたパスワード検証を行う。
// 配線とボーレートが正しいかの確認にそのまま使える。
func (d *Device) VerifyPassword(password uint32) error {
	status, _, err := d.Command([]byte{
		cmdVfyPwd,
		byte(password >> 24), byte(password >> 16), byte(password >> 8), byte(password),
	})
	if err != nil {
		return err
	}
	return status.Err()
}

// Match は照合で見つかったテンプレート。
type Match struct {
	// Page は指紋ライブラリ上の ID。
	Page uint16
	// Score は一致スコア。大きいほど確からしい。
	Score uint16
}

// SysPara はモジュールのシステムパラメータ。
type SysPara struct {
	// StatusRegister はモジュールのステータスレジスタ。
	StatusRegister uint16
	// SystemID はセンサー種別を示す固定値（AS608 では 0x0009）。
	SystemID uint16
	// Capacity は指紋ライブラリに格納できるテンプレート数。
	Capacity uint16
	// SecurityLevel は照合の厳しさ（1 が最も緩く 5 が最も厳しい）。
	SecurityLevel uint16
	// Address はモジュールアドレス。
	Address uint32
	// PacketSize は 1 データパケットの最大長（バイト）。
	PacketSize uint16
	// BaudRate は現在のボーレート（bps）。
	BaudRate uint32
}

// SystemParameters はモジュールのシステムパラメータを読み出す。
func (d *Device) SystemParameters() (SysPara, error) {
	status, data, err := d.Command([]byte{cmdReadSysPara})
	if err != nil {
		return SysPara{}, err
	}
	if err := status.Err(); err != nil {
		return SysPara{}, err
	}
	if len(data) < 16 {
		return SysPara{}, ErrBadPacket
	}

	return SysPara{
		StatusRegister: be16(data[0:]),
		SystemID:       be16(data[2:]),
		Capacity:       be16(data[4:]),
		SecurityLevel:  be16(data[6:]),
		Address:        uint32(data[8])<<24 | uint32(data[9])<<16 | uint32(data[10])<<8 | uint32(data[11]),
		// 実際のパケット長・ボーレートではなく設定値が入っている。
		PacketSize: 32 << be16(data[12:]),
		BaudRate:   9600 * uint32(be16(data[14:])),
	}, nil
}

// TemplateCount は登録済みテンプレートの数を返す。
func (d *Device) TemplateCount() (uint16, error) {
	status, data, err := d.Command([]byte{cmdTemplateNum})
	if err != nil {
		return 0, err
	}
	if err := status.Err(); err != nil {
		return 0, err
	}
	if len(data) < 2 {
		return 0, ErrBadPacket
	}
	return be16(data), nil
}

// CaptureImage はセンサー上の指を撮像して画像バッファに取り込む。
// 指が置かれていなければ StatusNoFinger を返す。
func (d *Device) CaptureImage() error {
	status, _, err := d.Command([]byte{cmdGetImage})
	if err != nil {
		return err
	}
	return status.Err()
}

// ExtractFeature は取り込んだ画像から特徴を抽出し、指定の特徴バッファへ入れる。
// 画像が不鮮明なら StatusImageTooDry、特徴点が足りなければ StatusImageTooWet を返す。
func (d *Device) ExtractFeature(buf Buffer) error {
	status, _, err := d.Command([]byte{cmdImg2Tz, byte(buf)})
	if err != nil {
		return err
	}
	return status.Err()
}

// CreateTemplate は 2 つの特徴バッファを統合して 1 件のテンプレートを作り、
// 両方のバッファに書き戻す。2 回の読み取りが別の指だと StatusMergeFailed を返す。
func (d *Device) CreateTemplate() error {
	status, _, err := d.Command([]byte{cmdRegModel})
	if err != nil {
		return err
	}
	return status.Err()
}

// StoreTemplate は特徴バッファのテンプレートを指紋ライブラリの page に保存する。
func (d *Device) StoreTemplate(buf Buffer, page uint16) error {
	status, _, err := d.Command([]byte{cmdStore, byte(buf), byte(page >> 8), byte(page)})
	if err != nil {
		return err
	}
	return status.Err()
}

// Search は特徴バッファを指紋ライブラリの start から count 件と照合する。
// 該当がなければ StatusNotFound を返す。
func (d *Device) Search(buf Buffer, start, count uint16) (Match, error) {
	status, data, err := d.Command([]byte{
		cmdSearch, byte(buf),
		byte(start >> 8), byte(start),
		byte(count >> 8), byte(count),
	})
	if err != nil {
		return Match{}, err
	}
	if err := status.Err(); err != nil {
		return Match{}, err
	}
	if len(data) < 4 {
		return Match{}, ErrBadPacket
	}
	return Match{Page: be16(data), Score: be16(data[2:])}, nil
}

// DeleteTemplate は page から count 件のテンプレートを削除する。
func (d *Device) DeleteTemplate(page, count uint16) error {
	status, _, err := d.Command([]byte{
		cmdDeleteChar,
		byte(page >> 8), byte(page),
		byte(count >> 8), byte(count),
	})
	if err != nil {
		return err
	}
	return status.Err()
}

// EmptyLibrary は指紋ライブラリを全消去する。
func (d *Device) EmptyLibrary() error {
	status, _, err := d.Command([]byte{cmdEmpty})
	if err != nil {
		return err
	}
	return status.Err()
}

// UsedPages は capacity 件までの範囲で使用中のページ ID を昇順に返す。
// モジュールによっては ReadIndexTable に対応しないため、エラーを無視して
// TemplateCount で代替できるようにしてある。
func (d *Device) UsedPages(capacity uint16) ([]uint16, error) {
	var pages []uint16

	for table := 0; uint16(table)*8*indexTableLen < capacity; table++ {
		status, data, err := d.Command([]byte{cmdReadIndexTable, byte(table)})
		if err != nil {
			return nil, err
		}
		if err := status.Err(); err != nil {
			return nil, err
		}
		if len(data) < indexTableLen {
			return nil, ErrBadPacket
		}

		for i, b := range data[:indexTableLen] {
			for bit := range 8 {
				if b&(1<<bit) == 0 {
					continue
				}
				page := uint16(table*8*indexTableLen + i*8 + bit)
				if page < capacity {
					pages = append(pages, page)
				}
			}
		}
	}

	return pages, nil
}

// readPacket はヘッダに同期してから 1 パケットを読み、チェックサムを検証する。
func (d *Device) readPacket() (PacketID, []byte, error) {
	deadline := time.Now().Add(d.Timeout)

	// ヘッダ 0xEF 0x01 に同期する。
	var prev byte
	for {
		b, err := d.readByte(deadline)
		if err != nil {
			return 0, nil, err
		}
		if prev == headerHi && b == headerLo {
			break
		}
		prev = b
	}

	var head [7]byte // アドレス(4) + パケット識別子(1) + 長さ(2)
	for i := range head {
		b, err := d.readByte(deadline)
		if err != nil {
			return 0, nil, err
		}
		head[i] = b
	}

	pid := PacketID(head[4])
	lenHi, lenLo := head[5], head[6]
	length := int(lenHi)<<8 | int(lenLo)
	if length < checksumLen || length-checksumLen > maxPayload {
		return 0, nil, ErrBadPacket
	}

	data := d.rx[:length-checksumLen]
	for i := range data {
		b, err := d.readByte(deadline)
		if err != nil {
			return 0, nil, err
		}
		data[i] = b
	}

	var sum [checksumLen]byte
	for i := range sum {
		b, err := d.readByte(deadline)
		if err != nil {
			return 0, nil, err
		}
		sum[i] = b
	}
	if uint16(sum[0])<<8|uint16(sum[1]) != checksum(pid, lenHi, lenLo, data) {
		return 0, nil, ErrChecksum
	}

	return pid, data, nil
}

// readByte は deadline まで待ちながら 1 バイト読む。
func (d *Device) readByte(deadline time.Time) (byte, error) {
	for {
		b, err := d.port.ReadByte()
		if err == nil {
			return b, nil
		}
		if !time.Now().Before(deadline) {
			return 0, ErrTimeout
		}
		time.Sleep(pollInterval)
	}
}

// drain は受信バッファに残っているバイトを読み捨てる。
func (d *Device) drain() {
	for range maxDrain {
		if _, err := d.port.ReadByte(); err != nil {
			return
		}
	}
}

func be16(b []byte) uint16 {
	return uint16(b[0])<<8 | uint16(b[1])
}
