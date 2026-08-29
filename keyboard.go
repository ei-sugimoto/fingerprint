package main

import (
	"machine/usb/hid/keyboard"
	"time"
)

// pinCode はビルド時に埋め込む Windows のロック解除用 PIN。
//
//	task build PIN=1234
//
// 空のままなら HID によるキー送出は一切行わない。
var pinCode string

const (
	// unlockCooldown は続けてキーを送らないための待ち時間。ロック解除済みの
	// PC で指を置いてしまったときに PIN を打ち込み続けるのを防ぐ。
	unlockCooldown = 15 * time.Second
	// keyDelay はキーストロークの間隔。詰めすぎるとホストが取りこぼす。
	keyDelay = 30 * time.Millisecond
	// wakeDelay はロック画面を起こしてから PIN を打ち始めるまでの待ち。
	wakeDelay = 500 * time.Millisecond
)

// lastUnlock は最後にキーを送った時刻。
var lastUnlock time.Time

// unlockEnabled はビルド時に PIN が埋め込まれているかを返す。
func unlockEnabled() bool {
	return pinCode != ""
}

// sendUnlock はロック画面を起こしてから PIN と Enter を送る。
// PIN が未設定か、クールダウン中なら何もせず false を返す。
func sendUnlock() bool {
	if !unlockEnabled() {
		return false
	}
	if !lastUnlock.IsZero() && time.Since(lastUnlock) < unlockCooldown {
		println("クールダウン中のため PIN は送りません。")
		return false
	}
	lastUnlock = time.Now()

	kb := keyboard.Port()

	// ロック画面は何かキーを押さないとサインイン欄が出てこない。
	if err := kb.Press(keyboard.KeySpace); err != nil {
		println("キーの送出に失敗しました:", err.Error())
		return false
	}
	time.Sleep(wakeDelay)

	for i := range len(pinCode) {
		if err := kb.WriteByte(pinCode[i]); err != nil {
			println("PIN の送出に失敗しました:", err.Error())
			return false
		}
		time.Sleep(keyDelay)
	}

	if err := kb.Press(keyboard.KeyEnter); err != nil {
		println("Enter の送出に失敗しました:", err.Error())
		return false
	}
	return true
}
