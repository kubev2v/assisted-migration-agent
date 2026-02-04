package main

import (
	"context"
)

type Observer struct {
	requests []Request
	input    chan Request
	out      chan chan Request
	cancel   context.CancelFunc
}

func NewObserver(input chan Request) *Observer {
	ctx, cancel := context.WithCancel(context.Background())
	obs := &Observer{
		requests: make([]Request, 0),
		input:    input,
		out:      make(chan chan Request),
		cancel:   cancel,
	}

	go obs.observe(ctx)

	return obs
}

func (o *Observer) Requests() []Request {
	req := []Request{}
	c := make(chan Request)
	o.out <- c
	for r := range c {
		req = append(req, r)
	}
	return req
}

func (o *Observer) Close() {
	if o.out != nil {
		close(o.out)
	}
	o.cancel()
}

func (o *Observer) observe(ctx context.Context) {
	for {
		select {
		case r, ok := <-o.input:
			if !ok {
				return
			}
			o.requests = append(o.requests, r)
		case c, ok := <-o.out:
			if !ok {
				return
			}
			for _, r := range o.requests {
				c <- r
			}
			close(c)
		case <-ctx.Done():
			return
		}
	}
}
