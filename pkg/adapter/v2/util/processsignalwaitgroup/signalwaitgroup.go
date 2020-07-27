package processsignalwaitgroup

import (
	"context"
	"sync"
	"time"
)

type processWaitGroup struct {
	waitgroup *sync.WaitGroup
}

var contextkey struct{}

func ContextWithProcessSignalWaitGroup(ctx context.Context) context.Context {

	if pwg, ok := fromContext(ctx); ok {
		return context.WithValue(ctx, contextkey, pwg)
	}

	return context.WithValue(ctx, contextkey, &processWaitGroup{
		waitgroup: &sync.WaitGroup{},
	})
}

func Add(ctx context.Context, delta int) {
	if pwg, ok := fromContext(ctx); ok {
		pwg.add(delta)
	}
}

func Done(ctx context.Context) {
	if pwg, ok := fromContext(ctx); ok {
		pwg.done()
	}
}

func Wait(ctx context.Context) {
	if pwg, ok := fromContext(ctx); ok {
		pwg.wait()
	}
}

func WaitTimeout(ctx context.Context, timeout time.Duration) {
	if pwg, ok := fromContext(ctx); ok {

		ch := make(chan bool, 1)
		defer close(ch)

		go func() {
			pwg.wait()
			ch <- true
		}()

		select {
		case <-ch:
		//	Read from ch
		case <-time.After(timeout):
			//Timed out"
		}

	}
}

func (pwg *processWaitGroup) add(delta int) {

	pwg.waitgroup.Add(delta)

}

func (pwg *processWaitGroup) done() {
	pwg.waitgroup.Done()

}

func (pwg *processWaitGroup) wait() {

	pwg.waitgroup.Wait()

}

func fromContext(ctx context.Context) (*processWaitGroup, bool) {
	wg, ok := ctx.Value(contextkey).(*processWaitGroup)
	return wg, ok
}
