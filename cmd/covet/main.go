package main

import (
	"fmt"
	"os"

	"covet/internal/cli"
)

func main() {
	// 过滤了第一个程序自身路径的参数
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
