package timingwheel_test

import (
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
	"time"
)

func Example_startTimer() {
	//tw := timingwheel.NewTimingWheel(time.Millisecond, 20)
	//tw.Start()
	//defer tw.Stop()
	//
	//exitC := make(chan time.Time, 1)
	//tw.AfterFunc(-time.Second, func() {
	//	fmt.Println("The timer fires")
	//	exitC <- time.Now().UTC()
	//})
	//
	//<-exitC

	// Output:
	// The timer fires
}

func Example_stopTimer() {
	tw := timingwheel.NewTimingWheel(time.Millisecond, 20)
	tw.Start()
	defer tw.Stop()

	tm := tw.AfterFunc(time.Second, func(t *timingwheel.Timer) {

	})

	<-time.After(900 * time.Millisecond)
	// Stop the timer before it fires
	tm.Stop()

	//Output:

}
