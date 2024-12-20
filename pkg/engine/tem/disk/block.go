package disk

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"sort"

	"github.com/szuwgh/hawkobserve/pkg/engine/tem/util/byteutil"
)

var (
	B  = 1
	KB = 1024 * B
	MB = 1024 * KB
	GB = 1024 * MB
)

const (
	blockTypeNoCompression     = 0
	blockTypeSnappyCompression = 1
)

var (
	blockTailLen = 5 * B
)

func sharedPrefixLen(a, b []byte) int {
	i, n := 0, len(a)
	if n > len(b) {
		n = len(b)
	}
	for i < n && a[i] == b[i] {
		i++
	}
	return i
}

type blockWriter struct {
	restartInterval int
	nEntries        int
	prevKey         []byte
	restarts        []uint32
	byteutil.EncBuf
	shareBuf []byte
}

func newBlockWriter(shareBuf []byte) *blockWriter {
	return &blockWriter{restartInterval: 1, shareBuf: shareBuf}
}

func (bw *blockWriter) append(key, value []byte) error {
	shareLen := 0
	if bw.restartInterval != 1 && bw.nEntries%bw.restartInterval == 0 {
		bw.restarts = append(bw.restarts, uint32(bw.Len()))
	} else {
		shareLen = sharedPrefixLen(bw.prevKey, key)
	}

	bw.PutUvarint64(uint64(shareLen))
	bw.PutUvarint64(uint64(len(key) - shareLen))
	bw.PutUvarint64(uint64(len(value)))
	//与前一条记录key非共享的内容
	if _, err := bw.Write(key[shareLen:], value); err != nil {
		return err
	} //写入数据

	bw.prevKey = append(bw.prevKey[:0], key...)
	bw.nEntries++
	return nil
}

func (bw *blockWriter) appendIndex(key []byte, bh blockHandle) error {
	n := encodeBlockHandle(bw.shareBuf[0:], bh)
	return bw.append(key, bw.shareBuf[:n])
}

//将restarts写入文件
func (bw *blockWriter) finishRestarts() {
	bw.restarts = append(bw.restarts, uint32(len(bw.restarts)))
	for _, x := range bw.restarts {
		buf4 := bw.Alloc(4)
		binary.LittleEndian.PutUint32(buf4, x)
	}
}

//将尾部写入文件
func (bw *blockWriter) finishTail() uint32 {
	tmp := bw.Alloc(blockTailLen)
	tmp[0] = blockTypeNoCompression
	checksum := crc32.ChecksumIEEE(bw.Get()[:bw.Len()-blockTailLen])
	binary.LittleEndian.PutUint32(tmp[1:], checksum)
	return checksum
}

//返回blockWriter长度
func (bw *blockWriter) bytesLen() int {
	return bw.Len() + len(bw.restarts)*4 + blockTailLen
}

func (bw *blockWriter) reset() {
	bw.Reset()
	bw.nEntries = 0
	bw.prevKey = bw.prevKey[:0]
	bw.restarts = bw.restarts[:0]
}

type blockReader struct {
	data           []byte
	offset         int
	free           int
	restartsLen    int
	restartsOffset int
}

// func (br *blockReader) read(offset, length uint64) []byte {
// 	return br.data[offset : offset+length]
// }

func (br *blockReader) search(key []byte) (offset int, err error) {
	index := sort.Search(br.restartsLen, func(i int) bool {
		offset := int(binary.LittleEndian.Uint32(br.data[br.restartsOffset+4*i:]))
		offset++
		v1, n1 := binary.Uvarint(br.data[offset:])   // key length
		_, n2 := binary.Uvarint(br.data[offset+n1:]) // value length
		m := offset + n1 + n2
		return bytes.Compare(br.data[m:m+int(v1)], key) > 0
	}) - 1
	offset = int(binary.LittleEndian.Uint32(br.data[br.restartsOffset+4*index:]))
	return
}

func (br *blockReader) entry(offset int) (key, value []byte, nShared, n int, err error) {
	v0, n0 := binary.Uvarint(br.data[offset:])       //与前一条记录key共享部分的长度
	v1, n1 := binary.Uvarint(br.data[offset+n0:])    //key 长度
	v2, n2 := binary.Uvarint(br.data[offset+n0+n1:]) //value长度
	m := n0 + n1 + n2
	n = m + int(v1) + int(v2)
	key = br.data[offset+m : offset+m+int(v1)]
	value = br.data[offset+m+int(v1) : offset+n]
	nShared = int(v0)
	return
}

func (br *blockReader) Seek(offset int) {
	br.offset = offset
}

func (br *blockReader) ReadByte() (byte, error) {
	if br.offset >= br.free {
		return 0, errors.New("no content readable")
	}
	b := br.data[br.offset]
	br.offset++
	return b, nil
}

func (br *blockReader) Release() {
	br.data = nil
}
