package as608

// Status はモジュールが応答パケットの先頭で返す確認コード。
type Status byte

// AS608 の確認コード。データシートに載るもののうち、本ドライバで
// 遭遇しうるものを定義している。
const (
	StatusOK                Status = 0x00 // 正常終了
	StatusPacketError       Status = 0x01 // パケット受信エラー
	StatusNoFinger          Status = 0x02 // センサー上に指がない
	StatusCaptureFailed     Status = 0x03 // 指紋画像の取り込みに失敗
	StatusImageTooDry       Status = 0x06 // 画像が不鮮明で特徴抽出に失敗
	StatusImageTooWet       Status = 0x07 // 特徴点が少なく特徴抽出に失敗
	StatusNoMatch           Status = 0x08 // 2 つの指紋が一致しない
	StatusNotFound          Status = 0x09 // 該当する指紋が見つからない
	StatusMergeFailed       Status = 0x0A // 特徴の統合に失敗
	StatusBadLocation       Status = 0x0B // ページ ID が指紋ライブラリの範囲外
	StatusReadTemplateFail  Status = 0x0C // テンプレートの読み出しに失敗
	StatusUploadFeatureFail Status = 0x0D // 特徴のアップロードに失敗
	StatusNoDataPacket      Status = 0x0E // 後続データパケットを受け付けられない
	StatusUploadImageFail   Status = 0x0F // 画像のアップロードに失敗
	StatusDeleteFail        Status = 0x10 // テンプレートの削除に失敗
	StatusClearFail         Status = 0x11 // 指紋ライブラリの全消去に失敗
	StatusWrongPassword     Status = 0x13 // パスワードが違う
	StatusNoValidImage      Status = 0x15 // バッファに有効な画像がない
	StatusFlashError        Status = 0x18 // Flash への書き込みエラー
	StatusInvalidRegister   Status = 0x1A // 無効なレジスタ番号
	StatusWrongAddress      Status = 0x20 // モジュールアドレスが違う
	StatusPasswordRequired  Status = 0x21 // パスワード検証が済んでいない
)

// Error は Status が error を満たすための実装。StatusOK に対しても
// 文字列を返すため、成否の判定には Err を使うこと。
func (s Status) Error() string {
	switch s {
	case StatusOK:
		return "as608: ok"
	case StatusPacketError:
		return "as608: packet receive error"
	case StatusNoFinger:
		return "as608: no finger on sensor"
	case StatusCaptureFailed:
		return "as608: failed to capture image"
	case StatusImageTooDry:
		return "as608: image too messy to extract features"
	case StatusImageTooWet:
		return "as608: too few feature points"
	case StatusNoMatch:
		return "as608: fingerprints do not match"
	case StatusNotFound:
		return "as608: no matching fingerprint found"
	case StatusMergeFailed:
		return "as608: failed to merge features"
	case StatusBadLocation:
		return "as608: page id out of range"
	case StatusReadTemplateFail:
		return "as608: failed to read template"
	case StatusUploadFeatureFail:
		return "as608: failed to upload feature"
	case StatusNoDataPacket:
		return "as608: cannot accept data packet"
	case StatusUploadImageFail:
		return "as608: failed to upload image"
	case StatusDeleteFail:
		return "as608: failed to delete template"
	case StatusClearFail:
		return "as608: failed to clear library"
	case StatusWrongPassword:
		return "as608: wrong password"
	case StatusNoValidImage:
		return "as608: no valid image in buffer"
	case StatusFlashError:
		return "as608: flash write error"
	case StatusInvalidRegister:
		return "as608: invalid register number"
	case StatusWrongAddress:
		return "as608: wrong module address"
	case StatusPasswordRequired:
		return "as608: password verification required"
	default:
		// 未定義のコードは原因の切り分けに値そのものが要る。
		return "as608: unknown confirmation code 0x" + hexByte(byte(s))
	}
}

const hexDigits = "0123456789ABCDEF"

func hexByte(b byte) string {
	return string([]byte{hexDigits[b>>4], hexDigits[b&0x0F]})
}

// Err は StatusOK なら nil を、それ以外なら Status 自身を error として返す。
func (s Status) Err() error {
	if s == StatusOK {
		return nil
	}
	return s
}
