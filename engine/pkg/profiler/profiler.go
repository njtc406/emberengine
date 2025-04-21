package profiler

import (
	"container/list"
	"fmt"
	"sync"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/utils/log"
)

// DefaultMaxOvertime 最大超时时间，一般可以认为是死锁或者死循环，或者极差的性能问题
var DefaultMaxOvertime time.Duration = 1 * time.Second

// DefaultOvertime 超过该时间将会提交监控报告
var DefaultOvertime time.Duration = 10 * time.Millisecond
var DefaultMaxRecordNum int = 100 //最大记录条数
var mapLock sync.RWMutex
var mapProfiler map[string]*Profiler

type ReportFunType func(name string, callNum int, costTime time.Duration, record *list.List)

var reportFunc ReportFunType = DefaultReportFunction

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

	analyzerPool sync.Pool
}

func init() {
	mapProfiler = map[string]*Profiler{}
}

func RegProfiler(profilerName string) *Profiler {
	mapLock.Lock()
	defer mapLock.Unlock()
	if _, ok := mapProfiler[profilerName]; ok == true {
		return nil
	}

	pProfiler := &Profiler{
		stack:       list.New(),
		record:      list.New(),
		maxOverTime: DefaultMaxOvertime,
		overTime:    DefaultOvertime,
		analyzerPool: sync.Pool{New: func() interface{} {
			return &Analyzer{}
		}},
	}
	mapProfiler[profilerName] = pProfiler
	return pProfiler
}

func UnRegProfiler(profilerName string) {
	mapLock.Lock()
	defer mapLock.Unlock()
	if _, ok := mapProfiler[profilerName]; !ok {
		return
	}
	delete(mapProfiler, profilerName)
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

	analyzer := slf.analyzerPool.Get().(*Analyzer)
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
	if slf.record.Len() >= DefaultMaxRecordNum {
		front := slf.stack.Front()
		if front != nil {
			slf.stack.Remove(front)
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
		slf.Reset()
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

func SetReportFunction(reportFun ReportFunType) {
	reportFunc = reportFun
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
	log.SysLogger.Infof("report: %s", strReport)
}

func Report() {
	var record *list.List
	for name, prof := range mapProfiler {
		prof.stackLocker.RLock()

		//取栈顶，是否存在异常MaxOverTime数据
		pElem := prof.stack.Back()
		for pElem != nil {
			pElement := pElem.Value.(*Element)
			pExceptionElem, _ := prof.check(pElement)
			if pExceptionElem != nil {
				prof.pushRecordLog(pExceptionElem)
			}
			pElem = pElem.Prev()
		}

		if prof.record.Len() == 0 {
			prof.stackLocker.RUnlock()
			continue
		}

		record = prof.record
		prof.record = list.New()
		callNum := prof.callNum
		totalCostTime := prof.totalCostTime
		prof.stackLocker.RUnlock()

		DefaultReportFunction(name, callNum, totalCostTime, record)
	}
}
