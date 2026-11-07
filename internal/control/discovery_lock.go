//go:build darwin || linux

package control

import (
	"errors"
	"os"
	"syscall"
)

// lockDiscovery は発見ファイルの隣のロックファイル（<path>.lock、0600）を排他でロックし、
// 解放する関数を返す。WriteDiscovery の置き換えと、RemoveDiscovery の「読む→token を比べる→消す」を
// このロックの中で行い、古いプロセスの Close が新しいプロセスの発見ファイルを消す競合を防ぐ。
//
// ロックファイルは消さずに残す。消すと、消す直前に別のプロセスが古いロックファイル（i-node）を
// 開いて待っている場合に、そのプロセスと、新しく作られたロックファイルをロックしたプロセスが
// 同時に中に入れてしまう。中身は空で、発見ファイルの形・場所・権限（kagelife-mcp との契約）は変えない。
//
// flock は darwin と linux で使える。Windows は ADR-003 で対象外。
func lockDiscovery(path string) (unlock func(), err error) {
	f, err := os.OpenFile(path+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = f.Close()

		return nil, err
	}

	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
