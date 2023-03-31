package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// TODO: add flags for each of the options and pass to newHeyFil
	httpIndexerEndpoint := flag.String("httpIndexerEndpoint", "https://cid.contact", "The HTTP IPNI endpoint to which announcements are made.")
	maxConcurrentChecks := flag.Int("maxConcurrentChecks", 10, "The maximum number of concurrent checks.")
	flag.Parse()

	hf, err := newHeyFil(WithHttpIndexerEndpoint(*httpIndexerEndpoint), WithMaxConcurrentChecks(*maxConcurrentChecks))
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
