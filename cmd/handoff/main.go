// Command handoff 是 handoff 的单二进制入口。
package main

import (
	"os"

	"github.com/yujinping/handoff/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
