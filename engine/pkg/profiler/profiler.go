package profiler

import (
	"container/list"
	"fmt"
	"sync"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/log"
	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
)

// TODO 这里面的整个内容应该是需要提到单独的模块中去做，所有监控信息都发送到监控模块中,统一写入日志
// TODO 如果做成服务，主要是logger的问题，输出可能就不是分开的了,做成模块,直接挂到每个服务中去

// DefaultMaxOvertime 最大超时时间，一般可以认为是死锁或者死循环，或者极差的性能问题
var DefaultMaxOvertime time.Duration = 1 * time.Second

// DefaultOvertime 超过该时间将会提交监控报告
var DefaultOvertime time.Duration = 10 * time.Millisecond
var DefaultMaxRecordNum int = 100 //最大记录条数

type ReportFunType func(name string, callNum int, costTime time.Duration, record *list.List)

type Element struct {
	tagName  string
	pushTime time.Time
}

type RecordType int

const (
	MaxOvertimeType = 1
	OvertimeType    = 2
)

type Record struct {
	RType      RecordType
	CostTime   time.Duration
	RecordName string
}

type Profiler struct {
	stack       *list.List //Element
	stackLocker sync.RWMutex
	mapAnalyzer map[*list.Element]Analyzer
	record      *list.List //Record

	callNum       int           //调用次数
	totalCostTime time.Duration //总消费时间长

	maxOverTime  time.Duration
	overTime     time.Duration
	maxRecordNum int

	analyzerPool pool.IPool[*Analyzer]
	logger       log.ILoggerX
}

func NewProfiler(logger log.ILoggerX) *Profiler {
	return &Profiler{
		stack:        list.New(),
		record:       list.New(),
		maxOverTime:  DefaultMaxOvertime,
		overTime:     DefaultOvertime,
		maxRecordNum: DefaultMaxRecordNum,
		analyzerPool: pool.NewSyncPoolWrapper[*Analyzer](
			func() *Analyzer {
				return &Analyzer{}
			},
			pool.NewNoStatsRecorder(),
			pool.WithReset(func(t *Analyzer) {
				t.Reset()
			}),
		),
		logger: logger,
	}
}

func (slf *Profiler) SetMaxOverTime(tm time.Duration) {
	slf.maxOverTime = tm
}

func (slf *Profiler) SetOverTime(tm time.Duration) {
	slf.overTime = tm
}

func (slf *Profiler) SetMaxRecordNum(num int) {
	slf.maxRecordNum = num
}

func (slf *Profiler) Push(tag string) *Analyzer {
	slf.stackLocker.Lock()
	defer slf.stackLocker.Unlock()

	pElem := slf.stack.PushBack(&Element{tagName: tag, pushTime: time.Now()}) // 使用真实时间

	analyzer := slf.analyzerPool.Get()
	analyzer.elem = pElem
	analyzer.profiler = slf

	return analyzer
}

func (slf *Profiler) check(pElem *Element) (*Record, time.Duration) {
	if pElem == nil {
		return nil, 0
	}

	subTm := time.Now().Sub(pElem.pushTime)
	if subTm < slf.overTime {
		return nil, subTm
	}

	record := Record{
		RType:      OvertimeType,
		CostTime:   subTm,
		RecordName: pElem.tagName,
	}

	if subTm > slf.maxOverTime {
		record.RType = MaxOvertimeType
	}

	return &record, subTm
}

func (slf *Profiler) pushRecordLog(record *Record) {
	if slf.record.Len() >= slf.maxRecordNum {
		front := slf.record.Front()
		if front != nil {
			slf.record.Remove(front)
		}
	}

	slf.record.PushBack(record)
}

type Analyzer struct {
	elem     *list.Element
	profiler *Profiler
}

func (slf *Analyzer) Reset() {
	slf.elem = nil
	slf.profiler = nil
}

func (slf *Analyzer) Pop() {
	slf.profiler.stackLocker.Lock()
	defer func() {
		slf.profiler.analyzerPool.Put(slf)
	}()
	defer slf.profiler.stackLocker.Unlock()

	pElement := slf.elem.Value.(*Element)
	pElem, subTm := slf.profiler.check(pElement)
	slf.profiler.callNum += 1
	slf.profiler.totalCostTime += subTm
	if pElem != nil {
		slf.profiler.pushRecordLog(pElem)
	}
	slf.profiler.stack.Remove(slf.elem)
}

func DefaultReportFunction(name string, callNum int, costTime time.Duration, record *list.List) {
	if record.Len() <= 0 {
		return
	}

	var strReport string
	strReport = "Profiler report tag " + name + ":\n"
	var average int64
	if callNum > 0 {
		average = costTime.Milliseconds() / int64(callNum)
	}

	strReport += fmt.Sprintf("process count %d,take time %d Milliseconds,average %d Milliseconds/per.\n", callNum, costTime.Milliseconds(), average)
	elem := record.Front()
	var strTypes string
	for elem != nil {
		pRecord := elem.Value.(*Record)
		if pRecord.RType == MaxOvertimeType {
			strTypes = "too slow process"
		} else {
			strTypes = "slow process"
		}

		strReport += fmt.Sprintf("%s:%s is take %d Milliseconds\n", strTypes, pRecord.RecordName, pRecord.CostTime.Milliseconds())
		elem = elem.Next()
	}

	// TODO 后面在看这个日志写在哪里
	fmt.Printf("report: %s", strReport)
}
