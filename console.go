package main

import (
	"errors"
	"machine"
	"strconv"
	"time"

	"github.com/ei-sugimoto/fingerprint/internal/as608"
)

// maxLineLen は 1 行の入力として受け付ける長さ。
const maxLineLen = 32

// errLibraryFull は空き ID が尽きたことを示す。
var errLibraryFull = errors.New("指紋ライブラリに空きがありません")

// lineBuf は改行が届くまでの入力を溜めておく。
var lineBuf []byte

// readLine はシリアルに届いた文字を溜め、1 行そろったら返す。
// そろっていなければ ok = false ですぐ戻る。
func readLine() (string, bool) {
	for machine.Serial.Buffered() > 0 {
		b, err := machine.Serial.ReadByte()
		if err != nil {
			break
		}

		switch {
		case b == '\r' || b == '\n':
			if len(lineBuf) == 0 {
				continue
			}
			line := string(lineBuf)
			lineBuf = lineBuf[:0]
			println()
			return line, true

		case b == 0x08 || b == 0x7F: // BS / DEL
			if len(lineBuf) > 0 {
				lineBuf = lineBuf[:len(lineBuf)-1]
				print("\b \b")
			}

		case b >= 0x20 && b < 0x7F && len(lineBuf) < maxLineLen:
			lineBuf = append(lineBuf, b)
			print(string([]byte{b})) // 端末に打った文字を返す
		}
	}
	return "", false
}

// waitLine は timeout まで 1 行の入力を待つ。
func waitLine(timeout time.Duration) (string, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if line, ok := readLine(); ok {
			return line, true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return "", false
}

// splitCommand は "d 3" のような行をコマンド名と引数に分ける。
func splitCommand(line string) (name, arg string) {
	for i := range len(line) {
		if line[i] == ' ' {
			return line[:i], trimSpace(line[i+1:])
		}
	}
	return line, ""
}

func trimSpace(s string) string {
	for len(s) > 0 && s[0] == ' ' {
		s = s[1:]
	}
	for len(s) > 0 && s[len(s)-1] == ' ' {
		s = s[:len(s)-1]
	}
	return s
}

// handleCommand は 1 行のコマンドを実行する。
func handleCommand(line string) {
	name, arg := splitCommand(trimSpace(line))

	switch name {
	case "h", "help":
		printHelp()
	case "e", "enroll":
		enroll(arg)
	case "l", "list":
		list()
	case "d", "delete":
		remove(arg)
	case "empty":
		empty()
	default:
		println("不明なコマンドです:", name, "（h でヘルプ）")
	}
}

func printHelp() {
	println("コマンド:")
	println("  e [ID]  指紋を登録する（ID 省略時は空きの若い番号）")
	println("  l       登録件数と使用中の ID を表示する")
	println("  d ID    指定した ID を削除する")
	println("  empty   全件削除する（確認あり）")
	println("  h       このヘルプ")
}

// enroll は同じ指を 2 回読み取って 1 件登録する。
func enroll(arg string) {
	page, err := resolvePage(arg)
	if err != nil {
		println("登録先の ID を決められませんでした:", err.Error())
		return
	}

	println("ID", page, "に登録します。")
	err = sensor.Enroll(page, as608.DefaultEnrollTimeout, func(step as608.EnrollStep) {
		switch step {
		case as608.StepPlaceFinger:
			println("  指を置いてください...")
		case as608.StepRemoveFinger:
			println("  指を離してください...")
		case as608.StepPlaceAgain:
			println("  もう一度、同じ指を置いてください...")
		case as608.StepMerge:
			println("  特徴を統合しています...")
		case as608.StepStore:
			println("  保存しています...")
		}
	})
	if err != nil {
		println("登録に失敗しました:", err.Error())
		return
	}
	println("ID", page, "に登録しました。")
}

// list は登録件数と使用中の ID を表示する。
func list() {
	count, err := sensor.TemplateCount()
	if err != nil {
		println("登録件数を読み出せませんでした:", err.Error())
		return
	}
	println("登録件数:", count, "/", capacity)

	pages, err := sensor.UsedPages(capacity)
	if err != nil {
		println("ID の一覧は取得できませんでした:", err.Error())
		return
	}
	if len(pages) == 0 {
		return
	}
	print("使用中の ID:")
	for _, p := range pages {
		print(" ", p)
	}
	println()
}

// remove は ID を指定して 1 件削除する。
func remove(arg string) {
	if arg == "" {
		println("ID を指定してください（例: d 3）")
		return
	}
	page, err := parsePage(arg)
	if err != nil {
		println("ID が読み取れませんでした:", arg)
		return
	}

	if err := sensor.DeleteTemplate(page, 1); err != nil {
		println("削除に失敗しました:", err.Error())
		return
	}
	println("ID", page, "を削除しました。")
}

// empty は確認を取ってから指紋ライブラリを全消去する。
func empty() {
	println("指紋ライブラリを全消去します。続けるなら yes と入力してください。")

	line, ok := waitLine(15 * time.Second)
	if !ok || trimSpace(line) != "yes" {
		println("中止しました。")
		return
	}

	if err := sensor.EmptyLibrary(); err != nil {
		println("全消去に失敗しました:", err.Error())
		return
	}
	println("全消去しました。")
}

// resolvePage は登録先の ID を決める。arg が空なら空きの若い番号を選ぶ。
func resolvePage(arg string) (uint16, error) {
	if arg != "" {
		return parsePage(arg)
	}
	return nextFreePage()
}

func parsePage(arg string) (uint16, error) {
	n, err := strconv.ParseUint(arg, 10, 16)
	if err != nil {
		return 0, err
	}
	if uint16(n) >= capacity {
		return 0, as608.StatusBadLocation
	}
	return uint16(n), nil
}

// nextFreePage は使われていない最小の ID を返す。ReadIndexTable に対応しない
// モジュールでは登録件数を次の ID として使う。
func nextFreePage() (uint16, error) {
	pages, err := sensor.UsedPages(capacity)
	if err != nil {
		count, cerr := sensor.TemplateCount()
		if cerr != nil {
			return 0, cerr
		}
		if count >= capacity {
			return 0, errLibraryFull
		}
		return count, nil
	}

	// pages は昇順なので、先頭から詰まっていない最初の番号が空き。
	next := uint16(0)
	for _, p := range pages {
		if p != next {
			break
		}
		next++
	}
	if next >= capacity {
		return 0, errLibraryFull
	}
	return next, nil
}
