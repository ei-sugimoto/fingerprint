# fingerprint

Raspberry Pi Pico 2 (RP2350) 向けの TinyGo ファームウェアです。
AS608 光学式指紋センサーを UART で接続し、ハンドシェイク・システムパラメータの読み出し・
指の検出までを実装しています。

## 必要なもの

| ツール | 用途 | 導入方法 |
| --- | --- | --- |
| [mise](https://mise.jdx.dev/) | go / tinygo / task / golangci-lint のバージョン管理 | 各自でインストール |
| [direnv](https://direnv.net/) | `.envrc` の自動読み込み（任意だが推奨） | 各自でインストール |
| Docker | 既定のビルド方法（TinyGo コンテナ） | 各自でインストール |
| [picotool](https://github.com/raspberrypi/picotool) | UF2 の書き込みとデバイス情報の取得 | 各自でインストール |

バージョンは `mise.toml` で固定しており、ローカルと CI で同じものを使います。

```
go            1.26.4
tinygo        0.41.1
golangci-lint 2.13.1
task          latest
```

## セットアップ

```sh
task setup
```

`mise install` でツールを導入し、`.envrc.example` を `.envrc` にコピーして `direnv allow` します。
`.envrc` は gitignore 済みなので、環境ごとの調整はそちらで行ってください。

`.envrc` は TinyGo が合成する GOROOT / GOOS / GOARCH / build tags を環境変数に流し込みます。
これにより `machine` など TinyGo 固有のパッケージを gopls や golangci-lint が解決できるようになります。

## 指紋センサー (AS608)

### ピン配置

AS608 は 3.3V TTL の UART でつながります。Pico 2 のロジックも 3.3V なので
レベルシフタは不要です。**TX と RX はクロスさせる**点にだけ注意してください。

| AS608 側 | 代表的な線色 | Pico 2 側 | 物理ピン |
| --- | --- | --- | --- |
| VCC | 赤 | 3V3(OUT) | 36 |
| TXD | 黄 | GP1 (UART0 RX) | 2 |
| RXD | 白 / 緑 | GP0 (UART0 TX) | 1 |
| GND | 黒 | GND | 3 |
| VT (タッチ検出電源) | 緑 | 3V3(OUT) | 36 |
| WAK / TCH (指検出出力) | 青 | 未接続（任意で GP2 など） | 4 |

線色はモジュールによって入れ替わることがあるので、基板のシルクを優先してください。
WAK は指が触れたときに High を出す信号で、割り込みで叩き起こす用途に使います。
本ファームウェアはポーリングで撮像しているため未接続で構いません。

```
Pico 2                       AS608
  GP0 (pin 1)  ──────────►  RXD
  GP1 (pin 2)  ◄──────────  TXD
  3V3 (pin 36) ──────────►  VCC / VT
  GND (pin 3)  ──────────   GND
```

### 電源

素の AS608 基板（3.3V 版）は **VCC が 3.0〜3.6V** で、5V を加えると壊れます。
Pico の 3V3(OUT) から取ってください。R307 のようにレギュレータを積んだモジュールは
4.2〜6V 入力なので、その場合は VBUS (40 番ピン) を使います。どちらか分からないときは
モジュールのデータシートを確認してください。

消費電流は動作時でも 60mA 程度で、Pico の 3V3 レギュレータ（300mA まで）で足ります。

### UART の設定

工場出荷時の設定は 57600bps / 8bit / パリティなし / ストップビット 1 です。
`main.go` では UART0 を使っています。別のピンに移したい場合は UART1 (GP8 / GP9) が使えます。

```go
uart.Configure(machine.UARTConfig{
	BaudRate: as608.DefaultBaudRate, // 57600
	TX:       machine.UART0_TX_PIN,  // GP0
	RX:       machine.UART0_RX_PIN,  // GP1
})
```

プロトコルの実装は `internal/as608` にあります。`machine` に依存せず
`Write` と `ReadByte` を持つポートを受け取るので、ホスト上でテストできます。

### 動作

書き込むと USB シリアル (`/dev/ttyACM0`) に疎通結果を出力します。

```sh
task flash
picocom -b 115200 --imap lfcrlf /dev/ttyACM0   # または screen / minicom
```

TinyGo の `println` は改行に LF しか出さないため、`--imap lfcrlf`（受信した LF を
CRLF として扱う）を付けないと行が階段状にずれたり、打った文字と次の行が重なったり
します。

```
AS608: ハンドシェイク成功
AS608: システム識別子 = 0
AS608: テンプレート容量 = 300
AS608: セキュリティレベル = 3
AS608: パケット長 = 128 バイト
AS608: ボーレート = 57600 bps
AS608: 登録済みテンプレート = 0
```

システム識別子はデータシート上 0x0009 固定ですが、実機は 0 を返しました。
判定には使わず参考値として表示しています。

ホストがシリアルを開く（DTR を上げる）まで最大 10 秒待ってから出力するので、
書き込み直後に `cat /dev/ttyACM0` を始めれば起動時のログを取りこぼしません。

初期化に失敗した場合は LED が速く点滅し、原因がシリアルに繰り返し出力されます。

### 指紋の登録と照合

起動後はシリアルがそのままコンソールになります。`picocom` などで開いてコマンドを打ちます。

| 入力 | 動作 |
| --- | --- |
| `e` / `e 5` | 指紋を登録する（ID 省略時は空きの若い番号を自動で選ぶ） |
| `l` | 登録件数と使用中の ID を表示する |
| `d 5` | ID 5 を削除する |
| `empty` | 全件削除する（`yes` の入力で確定） |
| `h` | ヘルプ |

登録は同じ指を 2 回読み取ります。各ステップの待ち時間は 30 秒です。

```
> e
ID 0 に登録します。
  指を置いてください...
  指を離してください...
  もう一度、同じ指を置いてください...
  特徴を統合しています...
  保存しています...
ID 0 に登録しました。
```

2 回が別の指だと `as608: failed to merge features` で失敗するので、同じ指を置き直してください。

コマンドを打っていない間は照合を回しています。登録済みの指を置くと LED が点灯し、
ID と一致スコアが出ます。

```
一致しました: ID = 0  スコア = 142
登録されていない指です。
```

一致の厳しさはセンサー側のセキュリティレベル（1〜5、既定 3）で決まります。

### Windows のロック解除 (HID)

PIN を埋め込んでビルドすると、指紋が一致したときに USB キーボードとして
`Space` → PIN → `Enter` を送ります。`machine/usb/hid/keyboard` を import すると
CDC と HID の複合デバイスになるため、**シリアルコンソールはそのまま使えます**。

```sh
task build PIN=1234 --force     # 埋め込んでビルド
task flash PIN=1234 --force     # 書き込みまで
```

`.envrc` に書いておくこともできます（`.envrc` は gitignore 済み）。

```sh
export UNLOCK_PIN=1234
```

`--force` が要るのは、Task が Go ファイルの変更しか見ておらず、PIN だけを変えても
再ビルドされないためです。

PIN を渡さずにビルドしたファームウェアはキーを一切送りません。起動時にどちらの
状態かが出ます。

```
PIN 送出: 有効（一致したらキーボードとして PIN と Enter を送ります）
PIN 送出: 無効（ビルド時に PIN が埋め込まれていません）
```

先頭の `Space` はロック画面からサインイン欄を出すためのものです。送出後は 15 秒の
クールダウンを置き、ロック解除済みの PC で指を置いてしまったときに PIN を打ち込み
続けないようにしています。

#### WSL2 では usbipd の切り離しが要る

`usbipd attach` で WSL に渡している間、この HID キーボードは Linux 側につながって
おり、**Windows にはキーが届きません**。ビルドと登録は WSL 側で行い、実際にロックを
解除するときは Windows 側へ戻してください。

```powershell
usbipd detach --busid <ID>
```

ただし `usbipd attach --auto-attach` を常駐させていると、detach しても即座に WSL へ
戻されます。**共有そのものを解除するのが確実です。**

```powershell
usbipd unbind --busid <ID>
```

書き込みのために WSL 側へ戻すときは bind からやり直します。

```powershell
usbipd bind --busid <ID>
usbipd attach --wsl --busid <ID>
```

Windows 側に戻っているかは、デバイスマネージャーに「USB Composite Device」と
「HID キーボード デバイス」が出ているかで判断できます。PowerShell なら次のとおりです。

```powershell
Get-PnpDevice -PresentOnly | Where-Object { $_.InstanceId -like '*VID_2E8A*' } |
  Select-Object Class,Status,FriendlyName
```

CDC だけ（`Ports` クラスのみ）が出ている場合は HID が認識されていません。

#### 承知しておくべきこと

1. **PIN はファームウェアに平文で載ります。** `picotool save` でフラッシュを吸い出せば
   読み取れます（`strings build/firmware.uf2` で確認できます）。RP2350 のセキュアブートと
   フラッシュ暗号化を使わない限り防げません。**このデバイスを持ち去られることは、
   PIN を渡すことと同じです。**
2. 指紋テンプレートはセンサー内の Flash にあります。センサーを別の Pico に繋ぎ替えれば
   同じ照合ができます。
3. AS608 は光学式で、Windows Hello 認定リーダーのようななりすまし対策はありません。

利便性のためのガジェットであって、セキュリティを高めるものではありません
（PIN を物理的に持ち歩くぶん、むしろ下がります）。

### つながらないときは

| 症状 | 確認するところ |
| --- | --- |
| ハンドシェイクがタイムアウトする | TX/RX が入れ替わっていないか、GND を共通にしているか |
| 応答が化ける | ボーレートが 57600 か（変更済みのモジュールは 9600 や 115200 のことがある） |
| `wrong password` が返る | パスワードが変更されている。`VerifyPassword` に設定済みの値を渡す |
| センサーの LED が光らない | VCC の電圧。3.3V 版に 5V を入れていないか |
| 登録で `timed out waiting for finger` | 30 秒以内に指が置かれなかった。置き直して `e` からやり直す |
| 登録で `failed to merge features` | 2 回の読み取りが別の指と判定された。同じ指を同じ向きで置く |

## ビルドと書き込み

```sh
task            # タスク一覧
task build      # build/firmware.uf2 を生成
task flash      # ビルドして picotool で書き込み、実行する
task info       # 接続中のデバイス情報を表示
task targets    # TinyGo が対応するターゲット一覧
task clean      # build/ と .cache/ を削除
```

ビルドは既定で Docker (`tinygo/tinygo:0.41.1`) を使います。mise で入れた TinyGo を直接使う場合は
`BUILDER=local` を渡してください。ターゲットは既定で `pico2` で、`TARGET=...` で変更できます。

```sh
task build BUILDER=local
task build TARGET=pico
```

### BOOTSEL ボタンを押さずに書き込む

TinyGo の USB CDC は、ホストが 1200bps でポートを開いて DTR を落とすと `machine.EnterBootloader()` を
呼びます。RP2350 では ROM の `reset_usb_boot()` に到達するため、1200bps タッチが BOOTSEL ボタンの
長押しの代わりになります。

```sh
task bootsel && task flash
```

`task bootsel` は再起動を要求したあと、picotool がデバイスを認識するまでポーリングします
（既定で最大 30 秒）。ポートやタイムアウトは変数で変更できます。

```sh
task bootsel PORT=/dev/ttyACM1 BOOTSEL_TIMEOUT=60
```

BOOTSEL モードに入るとデバイスは `2e8a:000a` から `2e8a:000f` に切り替わります。

### WSL2 での注意

WSL2 では USB デバイスを Windows 側から明示的にアタッチする必要があります。

```powershell
usbipd list
usbipd attach --wsl --busid <ID>
```

BOOTSEL モードへの遷移で VID:PID が変わるため、**再アタッチが必要**です。
`task bootsel` が待機時間を長めに取っているのはこのためで、書き込みが失敗する場合は
まず usbipd の接続状態を確認してください。

## Lint / テスト / フォーマット

```sh
task lint   # golangci-lint run
task fmt    # golangci-lint fmt (gofmt / goimports)
task test   # tinygo test ./...
```

`.golangci.yml` では構文レベルで完結する linter のみを有効にしています。
TinyGo の GOROOT 配下では gc が `//go:inline` を拒否するため、エクスポートデータのコンパイルを伴う
型情報ベースの linter（govet / staticcheck / errcheck / unused）は動作しません。

## その他のタスク

```sh
task shell  # TinyGo コンテナ内のシェルに入る
```

## CI

GitHub Actions (`.github/workflows/ci.yml`) で lint / test / build を実行し、
`build/firmware.uf2` を artifact としてアップロードします。
CI では 2.33GB の Docker イメージを pull せずに済むよう `task build BUILDER=local` を使っています。
