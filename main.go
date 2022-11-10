package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// TODO: add flags for each of the options and pass to newHeyFil
	hf, err := newHeyFil()
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := hf.Start(ctx); err != nil {
		panic(err)
	}
	defer func() {
		cancel()
		_ = hf.Shutdown(context.Background())
	}()

	exit := make(chan os.Signal, 1)
	signal.Notify(exit, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-exit
		cancel()
	}()
	<-ctx.Done()
}
