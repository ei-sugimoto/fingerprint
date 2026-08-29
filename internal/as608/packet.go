package as608

import "errors"

// PacketID はパケット種別を表す識別子。
type PacketID byte

const (
	// PacketCommand はホストからモジュールへの命令パケット。
	PacketCommand PacketID = 0x01
	// PacketData は後続がある転送データパケット。
	PacketData PacketID = 0x02
	// PacketAck はモジュールからホストへの応答パケット。
	PacketAck PacketID = 0x07
	// PacketEndData は転送の最後を示すデータパケット。
	PacketEndData PacketID = 0x08
)

const (
	headerHi byte = 0xEF
	headerLo byte = 0x01

	// checksumLen はパケット末尾のチェックサムの長さ。
	checksumLen = 2
	// headerLen はヘッダ(2) + アドレス(4) + パケット識別子(1) + 長さ(2)。
	headerLen = 9

	// maxPayload は本ドライバが扱うペイロードの最大長。画像転送を
	// 除けば AS608 の応答は最長でも 17 バイト (ReadSysPara) で足りる。
	maxPayload = 64

	// maxDrain は送信前に読み捨てる残留バイトの上限。
	maxDrain = 256
)

// ドライバが返すプロトコルレベルのエラー。
var (
	ErrTimeout      = errors.New("as608: timeout waiting for response")
	ErrChecksum     = errors.New("as608: checksum mismatch")
	ErrBadPacket    = errors.New("as608: malformed packet")
	ErrPayloadRange = errors.New("as608: payload length out of range")
)

// appendPacket は payload を 1 パケットに組み立てて dst に追記する。
func appendPacket(dst []byte, addr uint32, pid PacketID, payload []byte) []byte {
	length := len(payload) + checksumLen
	lenHi, lenLo := byte(length>>8), byte(length)

	dst = append(dst,
		headerHi, headerLo,
		byte(addr>>24), byte(addr>>16), byte(addr>>8), byte(addr),
		byte(pid),
		lenHi, lenLo,
	)
	dst = append(dst, payload...)

	sum := checksum(pid, lenHi, lenLo, payload)
	return append(dst, byte(sum>>8), byte(sum))
}

// checksum はパケット識別子・長さ・データの総和を返す。ヘッダとアドレスは含まない。
func checksum(pid PacketID, lenHi, lenLo byte, payload []byte) uint16 {
	sum := uint16(pid) + uint16(lenHi) + uint16(lenLo)
	for _, b := range payload {
		sum += uint16(b)
	}
	return sum
}
