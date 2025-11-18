package timingwheel_test

import (
	"fmt"
	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/timingwheel"
	"os"
	"time"
)

type EveryScheduler struct {
	Interval time.Duration
}

func (s *EveryScheduler) Next(prev time.Time) time.Time {
	return prev.Add(s.Interval)
}

var signCh = make(chan os.Signal, 1)

func printTask(t *timingwheel.Timer, args ...interface{}) error {
	fmt.Println(">>>>>>>>>>>>>task:", t.GetName())
	return nil
}

func Example_scheduleTimer() {
	logger, err := log.NewDefaultLogger("", nil, true)
	if err != nil {
		panic(err)
	}
	timingwheel.Start(time.Millisecond, 100, logger)
	defer timingwheel.Stop()
	var beginTime time.Time
	go func() {
		beginTime = time.Now()
		//tId, err := dp.AfterFunc(time.Second*5, printTask, nil, nil, "hello")
		//tId, err := dp.TickerFunc(time.Hour*3, printTask, nil, nil, "hello")
		tId, err := dp.CronFunc("0 */1 * * * *", "", printTask, "hello")
		if err != nil {
			fmt.Println("err:", err)
			dp.CancelTimer(tId)
		} else {
			fmt.Println("tId:", tId)
		}
	}()

	go func() {
		for {
			select {
			case job := <-dp.GetTimerCbChannel():
				fmt.Println("job:", job)
				job.Do()
				fmt.Println("sub time:", time.Now().Sub(beginTime))
				fmt.Println("now:", time.Now())
				//return
			}
		}
	}()
	<-signCh
	fmt.Println("main exit")
}
