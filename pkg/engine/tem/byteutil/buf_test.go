package byteutil

import (
	"fmt"
	"testing"
)

func Test_EncBuf(t *testing.T) {
	e := EncBuf{}
	e.PutUvarint(1234)

	d := NewDecBuf(e.Get())
	s := d.Uvarint()
	fmt.Println(s)
}
