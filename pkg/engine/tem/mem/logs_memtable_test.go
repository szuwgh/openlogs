package mem

import (
	"fmt"
	"testing"

	"github.com/szuwgh/hawkobserve/pkg/engine/tem/util/byteutil"
)

func Test_logs(t *testing.T) {
	alloc := byteutil.NewByteBlockStackAllocator()
	table := NewLogsTable(byteutil.NewForwardBytePool(alloc))
	table.WriteLog([]byte("aaaaaaaaaaaaaaaaaaaaa"))
	table.WriteLog([]byte("bbbbbbbbbbbbbbbbbbbb"))
	fmt.Println(table.ReadLog(1))
	fmt.Println(table.ReadLog(2))
}
