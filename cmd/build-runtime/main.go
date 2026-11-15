package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"cyberstrike-ai/internal/agentruntime"
)

func main() {
	root := flag.String("root", "runtime/pi", "Pi runtime source directory")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	path, err := agentruntime.PreparePiRuntime(ctx, *root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(path)
}
