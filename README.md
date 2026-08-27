# fingerprint

Raspberry Pi Pico 2 (RP2350) 向けの TinyGo ファームウェアです。
最終的には指紋センサーを扱うことを目的としていますが、現時点ではビルド／書き込み環境の整備と、
動作確認用の L チカ (`main.go`) までが実装されています。

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
