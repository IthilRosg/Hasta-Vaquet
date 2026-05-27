//go:build windows
package main

import (
	"fmt"
	"os"

	"golang.zx2c4.com/wintun"
)

func main() {
	adapter, err := wintun.OpenAdapter("HastaVaquet")
	if err != nil {
		fmt.Println("No adapter to clean up")
		os.Exit(0)
	}
	fmt.Println("Adapter found, closing...")
	adapter.Close()
	fmt.Println("Done")
}
