package mem

import (
	"bytes"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/szuwgh/athena/pkg/engine/athena/index"
	"github.com/szuwgh/athena/pkg/engine/athena/index/skiplist"
	"github.com/szuwgh/athena/pkg/lib/prompb"
	"github.com/szuwgh/athena/pkg/temql"
	"github.com/szuwgh/athena/pkg/tokenizer"

	"github.com/szuwgh/athena/pkg/engine/athena/chunks"
	"github.com/szuwgh/athena/pkg/engine/athena/disk"
	"github.com/szuwgh/athena/pkg/engine/athena/series"

	"github.com/szuwgh/athena/pkg/engine/athena/byteutil"

	"github.com/szuwgh/athena/pkg/engine/athena/global"
	"github.com/szuwgh/athena/pkg/engine/athena/posting"
	"github.com/szuwgh/athena/pkg/lib/prometheus/labels"
	//temqlLabels "github.com/szuwgh/athena/pkg/lib/prompb"
)

const (
	stripeSize = 1 << 14
	stripeMask = stripeSize - 1
)

type LogSummary struct {
	DocID  uint64
	Series *MemSeries
	Tokens tokenizer.Tokens
	Msg    []byte
	//	Lset      labels.Labels
	TimeStamp int64
}

type MetaIndex []int

//内存数据库
type MemTable struct {
	//mutex  sync.RWMutex
	symMtx sync.RWMutex

	indexs         indexGroup
	bytePool       byteutil.Inverted                //byteArrayPool
	bytePoolReader *byteutil.InvertedBytePoolReader //chunks.ChunkReader //*byteutil.InvertedBytePoolReader
	skLen          int
	baseTimeStamp  int64
	msgIndex       engineFunc
	nextWriteLogID int
	flushPosting   []*RawPosting
	series         *stripeSeries
	lastSeriesID   uint64

	logID uint64
}

func NewMemTable(bytePool byteutil.Inverted) *MemTable {
	mt := &MemTable{}
	mt.bytePool = bytePool
	mt.bytePoolReader = byteutil.NewInvertedBytePoolReader(bytePool, 0) //newByteBlockReader(bytePool)
	mt.indexs = NewDefalutTagGroup()
	mt.series = newStripeSeries()
	c := NewChain()
	c.Use(TermMiddleware())
	c.Use(LogFreqMiddleware())
	mt.msgIndex = c.Last(Position)
	return mt
}

func (mt *MemTable) newIndex(isTag bool) index.Index {
	return skiplist.New(isTag) //gorax.New(isTag) // //rax.New(isTag)
}

func (mt *MemTable) Init() {
	mt.indexs.Set(global.MESSAGE, mt.newIndex(false))
}

func (mt *MemTable) SetBaseTimeStamp(t int64) {
	mt.baseTimeStamp = t
	mt.bytePoolReader.SetBaseTime(t)
}

func (mt *MemTable) reset() {
	//mt.size = 0
	mt.lastSeriesID = 0
	mt.logID = 0
}

//NewBlock 申请一个新的内存块
func (mt *MemTable) NewBlock() uint64 {
	return mt.bytePool.InitBytes()
}

func (mt *MemTable) GetDataStructDB() indexGroup {
	return mt.indexs
}

func (mt *MemTable) ChunkReader() chunks.ChunkReader {
	return mt.bytePoolReader
}

//WriteString 写入字符串数据 返回写入的偏移
func (mt *MemTable) WriteString(i uint64, s string) (uint64, int) {
	return mt.bytePool.WriteString(i, s)
}

//WriteBytes 写入bytes
func (mt *MemTable) WriteBytes(i uint64, b []byte) (uint64, int) {
	return mt.bytePool.WriteBytes(i, b)
}

//WriteVInt 写入可变长编码
func (mt *MemTable) WriteVInt(i uint64, b int) (uint64, int) {
	return mt.bytePool.WriteVInt(i, b)
}

func (mt *MemTable) WriteVInt64(i uint64, b int64) (uint64, int) {
	return mt.bytePool.WriteVInt64(i, b)
}

func (mt *MemTable) WriteVUint64(i uint64, b uint64) (uint64, int) {
	return mt.bytePool.WriteVUint64(i, b)
}

func (mt *MemTable) ShowSeries() {

}

//回收内存
func (mt *MemTable) Close() error {
	// mt.bytePool.Release(recycle, alloced)
	// mt.indexs.Release()
	// mt.series.gc()
	// mt.reset()
	return nil
	//mt.indexs.
}

func (mt *MemTable) ReleaseBuff(recycle, alloced *int) error {
	mt.bytePool.Release(recycle, alloced)
	mt.indexs.Release()
	mt.series.gc()
	mt.reset()
	return nil
	//mt.indexs.
}

func (mt *MemTable) addLabel(s *MemSeries, t int64, v uint64) error {
	var offset uint64
	var size, length int
	//var size int = s.seriesLen
	offset, length = mt.bytePool.WriteVInt64(s.seriesIndex, t-s.lastTimeStamp)
	size += length
	offset, length = mt.bytePool.WriteVUint64(offset, v-s.lastLogID)
	size += length
	if s.minT == -1 {
		s.minT = t
	}
	s.maxT = t
	atomic.StoreUint64(&s.seriesIndex, offset)
	atomic.AddUint64(&s.seriesLen, uint64(size))

	s.lastTimeStamp = t
	s.lastLogID = v
	s.logNum++
	return nil
}

func (mt *MemTable) addTerm(context *Context, ref uint64, lset labels.Labels, pList index.Index) {
	pointer, ok := pList.Find(context.Term)
	//未出现过的词
	var posting *TermPosting
	if !ok {
		posting = newTermPosting()
		pList.Insert(context.Term, posting)
	} else {
		posting = pointer.(*TermPosting)
	}
	p, ok := posting.series[ref]
	if !ok {
		p = newRawPosting()
		p.lset = lset
		posting.series[ref] = p
	}
	context.P = p
	mt.msgIndex(context, mt)
	if !p.IsCommit {
		mt.flushPosting = append(mt.flushPosting, p)
		p.IsCommit = true
	}
}

func (mt *MemTable) getNextLogID() uint64 {
	mt.logID++
	return mt.logID
}

func (mt *MemTable) LogNum() uint64 {
	return mt.logID
}

//索引文档
func (mt *MemTable) Index(context *Context, logSum LogSummary) {
	//logID := mt.getNextLogID()
	s := logSum.Series
	context.LogID = logSum.DocID //mt.getNextLogID()
	context.TimeStamp = logSum.TimeStamp
	mt.addLabel(s, logSum.TimeStamp, logSum.DocID)

	//	tokens := a.Analyze([]byte(log.Msg))

	postingList, ok := mt.indexs.Get(global.MESSAGE) //e.processMap[global.MESSAGE]
	if !ok {
		return
	}
	//分词 全文索引
	for _, t := range logSum.Tokens {
		if strings.TrimSpace(t.Term) == "" {
			continue
		}
		context.Term = bytes.TrimSpace([]byte(t.Term)) //词
		context.Position = t.Position
		mt.addTerm(context, s.ref, logSum.Series.Lset(), postingList)
	}
	//atomic.AddUint64(&mt.size, uint64(log.Size()))
}

// func (mt *MemTable) Size() uint64 {
// 	return atomic.LoadUint64(&mt.size)
// }

func (mt *MemTable) GetOrCreate(hash uint64, lset labels.Labels) (*MemSeries, bool) {
	if len(lset) == 0 {
		return nil, false
	}
	s := mt.series.getByHash(hash, lset)
	if s != nil {
		return s, false
	}
	id := atomic.AddUint64(&mt.lastSeriesID, 1)

	return mt.getOrCreateWithID(id, hash, lset)
}

func (mt *MemTable) getOrCreateWithID(id, hash uint64, lset labels.Labels) (*MemSeries, bool) {

	s := newMemSeries(lset, id)
	s, created := mt.series.getOrSet(hash, s)
	if !created {
		return s, false
	}
	mt.symMtx.Lock()
	defer mt.symMtx.Unlock()
	s.byteStart = mt.bytePool.InitBytes()
	s.seriesIndex = s.byteStart
	for _, l := range lset {
		postingList, ok := mt.indexs.Get(l.Name)
		if !ok {
			postingList = mt.newIndex(true) //newSkipList(true)
			mt.indexs.Set(l.Name, postingList)
		}
		b := byteutil.Str2bytes(l.Value)
		pointer, ok := postingList.Find(b)
		var posting *LabelPosting
		if !ok {
			posting = &LabelPosting{}
			postingList.Insert(b, posting)
		} else {
			posting = pointer.(*LabelPosting)
		}
		posting.seriesID = append(posting.seriesID, id)
	}
	return s, true
}

func (mt *MemTable) Flush() {
	for _, p := range mt.flushPosting {
		WriteLogFreq(p, mt)
		ResetPosting(p, mt)
	}
	mt.flushPosting = mt.flushPosting[:0]
}

func (mt *MemTable) Iterator() disk.IteratorLabel {
	return mt.indexs.Iterator(mt.bytePoolReader, mt.series)
}

func (mt *MemTable) Search(lset []*prompb.LabelMatcher, expr temql.Expr) (posting.Postings, []series.Series) { // ([]*search.SeriesSnapShot, []*search.SnapShot) {
	var its []posting.Postings
	for _, v := range lset {
		postingList, ok := mt.indexs.Get(v.Name)
		if !ok {
			return posting.EmptyPostings, nil
		}
		its = append(its, selectSingle(postingList, byteutil.Str2bytes(v.Value)))
	}

	if expr == nil {
		p := posting.Intersect(its...)
		return p, []series.Series{mt.series}
	}
	postingList, ok := mt.indexs.Get(global.MESSAGE)
	if !ok {
		return posting.EmptyPostings, nil
	}
	var series []series.Series
	if len(its) > 0 {
		return posting.Intersect(queryTerm(expr, postingList, &series), posting.Intersect(its...)), series
	}
	return posting.Intersect(queryTerm(expr, postingList, &series)), series
}

func queryTerm(e temql.Expr, postingList index.Index, series *[]series.Series) posting.Postings {
	switch e.(type) {
	case *temql.TermBinaryExpr:
		expr := e.(*temql.TermBinaryExpr)
		p1 := queryTerm(expr.LHS, postingList, series)
		p2 := queryTerm(expr.RHS, postingList, series)
		switch expr.Op {
		case temql.LAND:
			return posting.Intersect(p1, p2)
		case temql.LOR:
			return posting.Merge(p1, p2)
		}
	case *temql.TermExpr:
		e := e.(*temql.TermExpr)
		pointer, _ := postingList.Find(byteutil.Str2bytes(e.Name))
		if pointer == nil {
			return posting.EmptyPostings
		}
		termList := pointer.(*TermPosting)
		*series = append(*series, termList)
		return posting.NewListPostings(termList.seriesID())
	}
	return nil
}

func selectSingle(p index.Index, b []byte) posting.Postings {
	m, _ := p.Find(b)
	if m == nil {
		return posting.EmptyPostings
	}
	list := m.(*LabelPosting)
	return posting.NewListPostings(list.seriesID)
}