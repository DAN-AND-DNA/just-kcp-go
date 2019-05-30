package main

import (
	"just-kcp-go/kcp"
	"log"
)

func main() {
	kcpcb := kcp.IkcpCreate(1, nil)
	_ = kcpcb

	var buffer []byte = make([]byte, 120)
	i := kcpcb.IkcpRecv(buffer)
	log.Println(i)

	i = kcpcb.IkcpSend(buffer)
	log.Println(i)
	z := -1
	m := (uint32)(z)
	log.Printf("%d", m)
}
