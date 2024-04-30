package utils

import (
	"github.com/banbox/banbot/core"
	"github.com/banbox/banexg/log"
	"github.com/schollz/progressbar/v3"
	"go.uber.org/zap"
	"sync"
)

type PrgBar struct {
	bar      *progressbar.ProgressBar
	m        *sync.Mutex
	doneNum  int
	totalNum int
	title    string
}

func NewPrgBar(totalNum int, title string) *PrgBar {
	pBar := progressbar.Default(int64(totalNum), title)
	return &PrgBar{
		bar:      pBar,
		m:        &sync.Mutex{},
		totalNum: totalNum,
		title:    title,
	}
}

func (p *PrgBar) Add(num int) {
	if p.bar == nil {
		return
	}
	p.m.Lock()
	defer p.m.Unlock()
	p.doneNum += num
	if p.doneNum > p.totalNum {
		log.Warn("pBar progress exceed", zap.String("title", p.title), zap.Int("max", p.totalNum),
			zap.Int("cur", p.doneNum))
		return
	}
	err_ := p.bar.Add(num)
	if err_ != nil {
		log.Error("add pBar fail", zap.String("title", p.title), zap.Error(err_))
	}
}

func (p *PrgBar) NewJob(num int) *PrgBarJob {
	return &PrgBarJob{PrgBar: p, jobTotal: num}
}

func (p *PrgBar) Close() {
	if p.bar == nil {
		return
	}
	if p.doneNum < p.totalNum {
		p.Add(p.totalNum - p.doneNum)
	}
	err := p.bar.Close()
	if err != nil {
		log.Error("close progressBar error", zap.Error(err))
	}
	p.bar = nil
}

type PrgBarJob struct {
	*PrgBar
	jobTotal  int // 当前子任务总数量
	jobDone   int // 当前子任务已完成数量
	jobPrgNum int // 对应StepTotal的进度值
}

func (j *PrgBarJob) Add(num int) {
	if j.PrgBar == nil || num <= 0 || j.jobDone >= j.jobTotal {
		return
	}
	j.jobDone += num
	curProgress := j.jobDone * core.StepTotal / j.jobTotal
	addNum := curProgress - j.jobPrgNum
	if addNum > 0 {
		j.PrgBar.Add(addNum)
	}
	j.jobPrgNum = curProgress
}

func (j *PrgBarJob) Done() {
	if j.jobPrgNum < core.StepTotal {
		j.Add(core.StepTotal - j.jobPrgNum)
		j.jobPrgNum = core.StepTotal
	}
}
