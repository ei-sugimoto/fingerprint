package main

import (
	"errors"
	"machine"
	"time"

	"github.com/ei-sugimoto/fingerprint/internal/as608"
)

// AS608 は UART0 につなぐ。配線は README を参照。
//
//	Pico 2 GP0 (UART0 TX, 1 番ピン) -> AS608 RXD
//	Pico 2 GP1 (UART0 RX, 2 番ピン) <- AS608 TXD
//	Pico 2 3V3 (36 番ピン)          -> AS608 VCC
//	Pico 2 GND (3 番ピン)           -> AS608 GND
const (
	sensorTXPin = machine.UART0_TX_PIN
	sensorRXPin = machine.UART0_RX_PIN
)

var (
	led    = machine.LED
	uart   = machine.UART0
	sensor = as608.New(uart)
)

func main() {
	led.Configure(machine.PinConfig{Mode: machine.PinOutput})

	// ホストがシリアルを開くまでのログは捨てられるので待つ。繋がなければ
	// そのまま進む。
	waitForHost(10 * time.Second)

	if err := uart.Configure(machine.UARTConfig{
		BaudRate: as608.DefaultBaudRate,
		TX:       sensorTXPin,
		RX:       sensorRXPin,
	}); err != nil {
		fail("UART の設定に失敗しました", err)
	}

	// パスワード検証はハンドシェイクを兼ねる。ここが通れば配線と
	// ボーレートは正しい。
	if err := sensor.VerifyPassword(as608.DefaultPassword); err != nil {
		fail("AS608 とのハンドシェイクに失敗しました", err)
	}
	println("AS608: ハンドシェイク成功")

	para, err := sensor.SystemParameters()
	if err != nil {
		fail("システムパラメータの読み出しに失敗しました", err)
	}
	println("AS608: システム識別子 =", para.SystemID)
	println("AS608: テンプレート容量 =", para.Capacity)
	println("AS608: セキュリティレベル =", para.SecurityLevel)
	println("AS608: パケット長 =", para.PacketSize, "バイト")
	println("AS608: ボーレート =", para.BaudRate, "bps")

	count, err := sensor.TemplateCount()
	if err != nil {
		fail("テンプレート数の読み出しに失敗しました", err)
	}
	println("AS608: 登録済みテンプレート =", count)

	// 疎通確認ができたので、指が置かれたら知らせるだけのループに入る。
	watchFinger()
}

// waitForHost はホストがシリアルポートを開く（DTR を上げる）まで待つ。
// timeout を過ぎたら諦めて先に進む。
func waitForHost(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for !machine.Serial.DTR() {
		if !time.Now().Before(deadline) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	// 開いた直後は数バイト落ちることがあるので少し置く。
	time.Sleep(200 * time.Millisecond)
}

// watchFinger はセンサーを撮像し続け、指が置かれている間 LED を点灯する。
func watchFinger() {
	for {
		err := sensor.CaptureImage()
		switch {
		case err == nil:
			led.High()
			println("AS608: 指を検出しました")
		case errors.Is(err, as608.StatusNoFinger):
			led.Low()
		default:
			led.Low()
			println("AS608: 撮像に失敗しました:", err.Error())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// fail はエラーを繰り返し報告しながら LED を速く点滅させる。
func fail(msg string, err error) {
	for {
		println("AS608 エラー:", msg, "-", err.Error())
		for range 5 {
			led.High()
			time.Sleep(100 * time.Millisecond)
			led.Low()
			time.Sleep(100 * time.Millisecond)
		}
	}
}
