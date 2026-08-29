package as608

import (
	"errors"
	"time"
)

// ErrFingerTimeout は指が置かれる／離れるのを待ちきれなかったことを示す。
// 応答自体が返らない ErrTimeout とは区別する。
var ErrFingerTimeout = errors.New("as608: timed out waiting for finger")

// EnrollStep は登録の進行段階。UI に「指を置いてください」などを出すために使う。
type EnrollStep int

const (
	// StepPlaceFinger は 1 回目の指を待っている段階。
	StepPlaceFinger EnrollStep = iota
	// StepRemoveFinger は指を離すのを待っている段階。
	StepRemoveFinger
	// StepPlaceAgain は 2 回目の指を待っている段階。
	StepPlaceAgain
	// StepMerge は 2 回ぶんの特徴を統合している段階。
	StepMerge
	// StepStore は Flash に書き込んでいる段階。
	StepStore
)

// DefaultEnrollTimeout は登録の各段階で指を待つ既定の上限。
// 置く・離す・もう一度置く、を人が落ち着いて操作できる長さにしてある。
const DefaultEnrollTimeout = 30 * time.Second

// Enroll は同じ指を 2 回読み取って 1 件のテンプレートを作り、page に保存する。
// 段階が変わるたびに notify を呼ぶ（nil でもよい）。
//
// 2 回の読み取りが別の指だった場合は StatusMergeFailed を返す。
func (d *Device) Enroll(page uint16, timeout time.Duration, notify func(EnrollStep)) error {
	step := func(s EnrollStep) {
		if notify != nil {
			notify(s)
		}
	}

	step(StepPlaceFinger)
	if err := d.WaitForFinger(timeout); err != nil {
		return err
	}
	if err := d.ExtractFeature(Buffer1); err != nil {
		return err
	}

	step(StepRemoveFinger)
	if err := d.WaitForRemoval(timeout); err != nil {
		return err
	}

	step(StepPlaceAgain)
	if err := d.WaitForFinger(timeout); err != nil {
		return err
	}
	if err := d.ExtractFeature(Buffer2); err != nil {
		return err
	}

	step(StepMerge)
	if err := d.CreateTemplate(); err != nil {
		return err
	}

	step(StepStore)
	return d.StoreTemplate(Buffer1, page)
}

// Identify は撮像から検索までを 1 回試す。指が置かれていなければ StatusNoFinger を、
// 登録済みのどれとも一致しなければ StatusNotFound を返す。
func (d *Device) Identify(capacity uint16) (Match, error) {
	if err := d.CaptureImage(); err != nil {
		return Match{}, err
	}
	if err := d.ExtractFeature(Buffer1); err != nil {
		return Match{}, err
	}
	return d.Search(Buffer1, 0, capacity)
}

// WaitForFinger は指が置かれて撮像できるまで待つ。
func (d *Device) WaitForFinger(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := d.CaptureImage()
		if !errors.Is(err, StatusNoFinger) {
			// 撮像できたか、指がない以外の理由で失敗したか。
			return err
		}
		if !time.Now().Before(deadline) {
			return ErrFingerTimeout
		}
		time.Sleep(d.FingerPollInterval)
	}
}

// WaitForRemoval は指が離れるまで待つ。
func (d *Device) WaitForRemoval(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := d.CaptureImage()
		switch {
		case errors.Is(err, StatusNoFinger):
			return nil
		case err == nil, errors.Is(err, StatusCaptureFailed):
			// まだ乗っている。離す途中は取り込みに失敗しやすいので待ち続ける。
		default:
			return err
		}
		if !time.Now().Before(deadline) {
			return ErrFingerTimeout
		}
		time.Sleep(d.FingerPollInterval)
	}
}
