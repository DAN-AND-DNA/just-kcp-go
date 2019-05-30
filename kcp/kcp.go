package kcp

const (
	IKCP_RTO_NDL     = 30    // no delay min rto
	IKCP_RTO_MIN     = 100   // min rto
	IKCP_RTO_DEF     = 200   // default rto
	IKCP_RTO_MAX     = 60000 //
	IKCP_CMD_PUSH    = 81    // cmd:push
	IKCP_CMD_ACK     = 82    // cmd:ack
	IKCP_CMD_WASK    = 83    // cmd:window probe (ask)
	IKCP_CMD_WINS    = 84    // cmd:window size (tell)
	IKCP_ASK_SEND    = 1     // need to send IKCP_CMD_WASK
	IKCP_ASK_TELL    = 2     // need to send IKCP_CMD_WINS
	IKCP_WND_SND     = 32
	IKCP_WND_RCV     = 128
	IKCP_INTERVAL    = 100 // tick 10ms
	IKCP_OVERHEAD    = 24  // kcp head size
	IKCP_DEADLINK    = 20
	IKCP_MTU_DEF     = 1400 // mtu
	IKCP_THRESH_INIT = 2
	IKCP_THRESH_MIN  = 2
	IKCP_PROBE_INIT  = 7000   // 7 secs to probe window size
	IKCP_PROBE_LIMIT = 120000 // up to 120 secs to probe window
)

type ikcpseg struct {
	m_dwConv uint32
	m_dwCmd  uint8
	m_dwFrg  uint8
	m_dwWnd  uint16
	m_dwTs   uint32
	m_dwSn   uint32
	m_dwUna  uint32

	m_dwResendTs uint32
	m_dwRto      uint32
	m_dwFastAck  uint32
	m_bAcked     bool
	m_dwXmit     uint32
	m_stData     []byte
}

func ikcpSegmentNew(size int) ikcpseg {
	return ikcpseg{
		m_stData: make([]byte, size),
	}
}

func ikcpSegmentDelete(seg *ikcpseg) {
	if seg == nil {
		return
	}
	seg.m_stData = nil
}

func (this *ikcpseg) ikcpEncodeSeg(ptr []byte) []byte {
	ptr = ikcpEncode32u(ptr, this.m_dwConv)
	ptr = ikcpEncode8u(ptr, this.m_dwCmd)
	ptr = ikcpEncode8u(ptr, this.m_dwFrg)
	ptr = ikcpEncode16u(ptr, this.m_dwWnd)
	ptr = ikcpEncode32u(ptr, this.m_dwTs)
	ptr = ikcpEncode32u(ptr, this.m_dwSn)
	ptr = ikcpEncode32u(ptr, this.m_dwUna)
	ptr = ikcpEncode32u(ptr, (uint32)(len(this.m_stData)))
	return ptr
}

type outputCallback func(buffer []byte, len int)

type ackItem struct {
	m_dwSn uint32
	m_dwTs uint32
}
type Ikcpcb struct {
	m_dwConv      uint32
	m_dwMtu       uint32
	m_dwMss       uint32
	m_dwState     uint32
	m_dwSndUna    uint32
	m_dwSndNxt    uint32
	m_dwRcvNxt    uint32
	m_dwSsthresh  uint32
	m_iRttVal     int
	m_iSrtt       int
	m_iRto        int
	m_iMinRto     int
	m_dwSndWnd    uint32
	m_dwRcvWnd    uint32
	m_dwRmtWnd    uint32
	m_dwCwnd      uint32
	m_dwProbe     uint32
	m_dwInterval  uint32
	m_bNoDelay    bool
	m_dwTsFlush   uint32
	m_dwXmit      uint32
	m_dwTsProbe   uint32
	m_dwProbeWait uint32
	m_dwDeadLink  uint32
	m_dwIncr      uint32
	m_stSndQueue  []ikcpseg
	m_stRcvQueue  []ikcpseg
	m_stSndBuf    []ikcpseg
	m_stRcvBuf    []ikcpseg
	m_stAckList   []ackItem
	m_stBuffer    []byte
	m_iFastResend int
	m_bNoCwnd     bool
	m_stOutput    outputCallback
}

func IkcpCreate(conv uint32, output outputCallback) *Ikcpcb {
	return &Ikcpcb{
		m_dwConv:     conv,
		m_dwMtu:      IKCP_MTU_DEF,
		m_dwMss:      IKCP_MTU_DEF - IKCP_OVERHEAD,
		m_dwSsthresh: IKCP_THRESH_INIT,
		m_dwSndWnd:   IKCP_WND_SND,
		m_dwRcvWnd:   IKCP_WND_RCV,
		m_dwRmtWnd:   IKCP_WND_RCV,
		m_iRto:       IKCP_RTO_DEF,
		m_iMinRto:    IKCP_RTO_MIN,
		m_dwInterval: IKCP_INTERVAL,
		m_dwTsFlush:  IKCP_INTERVAL,
		m_dwDeadLink: IKCP_DEADLINK,
		m_stBuffer:   make([]byte, (IKCP_MTU_DEF)),
		m_stOutput:   output,
	}
}

// check the size of next message in the recv queue

// Return false if there is no readable data
func (this *Ikcpcb) IkcpPeekSize() int {
	if this == nil {
		return -1
	}

	if len(this.m_stRcvQueue) == 0 {
		return -1

	}

	pstSeg := &this.m_stRcvQueue[0]
	if pstSeg.m_dwFrg == 0 {
		return len(pstSeg.m_stData)
	}

	if len(this.m_stRcvQueue) < (int)(pstSeg.m_dwFrg+1) {
		return -1
	}

	length := 0
	for k := range this.m_stRcvQueue {
		pstSeg := &this.m_stRcvQueue[k]
		length += len(pstSeg.m_stData)
		if pstSeg.m_dwFrg == 0 {
			break
		}
	}
	return length
}

func removeFront(q []ikcpseg, n int) []ikcpseg {
	if n > cap(q)/2 {
		newn := copy(q, q[n:])
		return q[:newn]
	}
	return q[n:]
}

// user/upper level recv: returns size, returns below zero for EAGAIN
func (this *Ikcpcb) IkcpRecv(buffer []byte) int {
	peeksize := this.IkcpPeekSize()
	if peeksize < 0 {
		return -1
	}

	if peeksize > len(buffer) {
		return -2
	}

	var fastRecover bool

	if len(this.m_stRcvQueue) >= (int)(this.m_dwRcvWnd) {
		fastRecover = true
	}

	// merge fragment
	count := 0
	length := 0
	for k := range this.m_stRcvQueue {
		pstSeg := &this.m_stRcvQueue[k]
		copy(buffer, pstSeg.m_stData)
		buffer = buffer[len(pstSeg.m_stData):]
		length += len(pstSeg.m_stData)
		count++
		ikcpSegmentDelete(pstSeg)
		if pstSeg.m_dwFrg == 0 {
			break
		}
	}

	if count > 0 {
		this.m_stRcvQueue = removeFront(this.m_stRcvQueue, count)
	}

	// move available data from rcv_buf -> rcv_queue
	count = 0
	for k := range this.m_stRcvBuf {
		pstSeg := &this.m_stRcvBuf[k]
		if pstSeg.m_dwSn == this.m_dwRcvNxt && len(this.m_stRcvQueue) < (int)(this.m_dwRcvWnd) {
			this.m_dwRcvNxt++
			count++
		} else {
			break
		}
	}

	if count > 0 {
		this.m_stRcvQueue = append(this.m_stRcvQueue, this.m_stRcvBuf[:count]...)
		this.m_stRcvBuf = removeFront(this.m_stRcvBuf, count)
	}

	// fast recover
	if len(this.m_stRcvQueue) < int(this.m_dwRcvWnd) && fastRecover {
		// ready to send back IKCP_CMD_WINS in ikcp_flush
		// tell remote my window size
		this.m_dwProbe |= IKCP_ASK_TELL
	}
	return length
}

// user/upper level send, returns below zero for error
func (this *Ikcpcb) IkcpSend(buffer []byte) int {

	length := len(buffer)
	if length == 0 {
		return -1
	}
	count := 0
	if length <= (int)(this.m_dwMss) {
		count = 1
	} else {
		count = ((length - 1) / (int)(this.m_dwMss)) + 1
	}

	if count > IKCP_WND_RCV {
		return -2
	}

	if count == 0 {
		count = 1
	}

	// fragment
	for i := 0; i < count; i++ {
		size := 0
		if len(buffer) > (int)(this.m_dwMss) {
			size = (int)(this.m_dwMss)
		} else {
			size = len(buffer)
		}

		seg := ikcpseg{
			m_stData: make([]byte, size),
		}

		copy(seg.m_stData, buffer[:size])
		seg.m_dwFrg = 0
		this.m_stSndQueue = append(this.m_stSndQueue, seg)
		buffer = buffer[size:]
	}
	return 0
}

// when you received a low level packet (eg. UDP packet), call it
func (this *Ikcpcb) IkcpInput(data []byte) int {
	if len(data) < IKCP_OVERHEAD {
		return -1
	}

	snduna := this.m_dwSndUna
	flag := false
	var maxAck uint32
	for {
		if len(data) < IKCP_OVERHEAD {
			break
		}

		var conv uint32
		var cmd, frg uint8
		var wnd uint16
		var ts, sn, una, length uint32

		data = ikcpDecode32u(data, &conv)
		data = ikcpDecode8u(data, &cmd)
		data = ikcpDecode8u(data, &frg)
		data = ikcpDecode16u(data, &wnd)
		data = ikcpDecode32u(data, &ts)
		data = ikcpDecode32u(data, &sn)
		data = ikcpDecode32u(data, &una)
		data = ikcpDecode32u(data, &length)

		if len(data) < (int)(length) {
			return -2
		}

		if cmd != IKCP_CMD_PUSH && cmd != IKCP_CMD_ACK &&
			cmd != IKCP_CMD_WASK && cmd != IKCP_CMD_WINS {
			return -3
		}

		this.m_dwRmtWnd = (uint32)(wnd)

		// remove segments as una
		this.ikcpParseUna(una)
		this.ikcpShrinkBuf()

		if cmd == IKCP_CMD_ACK {
			current := currentMs()
			if itimediff(current, ts) >= 0 {
				// predict rto & rtt
				this.ikcpUpdateAck(itimediff(current, ts))
			}

			// mark segment as ack but free at ikcpParseUna
			this.ikcpParseAck(sn)
			this.ikcpShrinkBuf()
			if flag == false {
				flag = true
				maxAck = sn
			} else {
				if itimediff(sn, maxAck) > 0 {
					maxAck = sn
				}
			}
		} else if cmd == IKCP_CMD_PUSH {
			if itimediff(sn, this.m_dwRcvNxt+this.m_dwRcvWnd) < 0 {
				this.ikcpAckPush(sn, ts)
				if itimediff(sn, this.m_dwRcvNxt) >= 0 {
					this.ikcpParseData(ikcpseg{
						m_dwConv: conv,
						m_dwCmd:  cmd,
						m_dwFrg:  frg,
						m_dwWnd:  wnd,
						m_dwTs:   ts,
						m_dwSn:   sn,
						m_dwUna:  una,
						m_stData: data[:length],
					})
				}
			}
		} else if cmd == IKCP_CMD_WASK {
			// ready to send back IKCP_CMD_WINS in ikcp_flush
			// tell remote my window size
			this.m_dwProbe |= IKCP_ASK_TELL
		} else if cmd == IKCP_CMD_WINS {
			// do nothing
		} else {
			return -3
		}
		data = data[length:]
	}

	if flag {
		this.ikcpParseFastAck(maxAck)
	}

	if itimediff(this.m_dwSndUna, snduna) > 0 {
		// the number of segments in SndBuf is changed (new segment arrived)

		if this.m_dwCwnd < this.m_dwRmtWnd {
			mss := this.m_dwMss
			if this.m_dwCwnd < this.m_dwSsthresh {
				this.m_dwCwnd++
				this.m_dwIncr += mss
			} else {
				if this.m_dwIncr < mss {
					this.m_dwIncr = mss
				}
				this.m_dwIncr += (mss*mss)/this.m_dwIncr + mss/16
			}

			if this.m_dwCwnd > this.m_dwRmtWnd {
				this.m_dwCwnd = this.m_dwRmtWnd
				this.m_dwIncr = this.m_dwRmtWnd * mss
			}
		}
	}

	return 0
}

func (this *Ikcpcb) IkcpFlush() {
	buffer := this.m_stBuffer
	ptr := buffer

	var seg ikcpseg
	seg.m_dwConv = this.m_dwConv
	seg.m_dwCmd = IKCP_CMD_ACK
	seg.m_dwFrg = 0
	seg.m_dwWnd = this.ikcpWndUnsed()
	seg.m_dwUna = this.m_dwRcvNxt

	makeSpace := func(space int) {
		size := len(buffer) - len(ptr) // used size
		if size+space > (int)(this.m_dwMtu) {
			this.m_stOutput(buffer, size)
			ptr = buffer
		}
	}

	// flush acknowledges
	for _, ack := range this.m_stAckList {
		makeSpace(IKCP_OVERHEAD)
		seg.m_dwSn, seg.m_dwTs = ack.m_dwSn, ack.m_dwTs
		ptr = seg.ikcpEncodeSeg(ptr)
	}

	this.m_stAckList = this.m_stAckList[0:0]

	// probe window size (if remote window size equals zero)
	if this.m_dwRmtWnd == 0 {
		current := currentMs()
		if this.m_dwProbeWait == 0 {
			this.m_dwProbeWait = IKCP_PROBE_INIT
			this.m_dwTsProbe = current + this.m_dwProbeWait
		} else {
			if itimediff(current, this.m_dwTsProbe) >= 0 {
				if this.m_dwProbeWait < IKCP_PROBE_INIT {
					this.m_dwProbeWait = IKCP_PROBE_INIT
				}

				this.m_dwProbeWait += this.m_dwProbeWait / 2
				if this.m_dwProbeWait > IKCP_PROBE_LIMIT {
					this.m_dwProbeWait = IKCP_PROBE_LIMIT
				}
				this.m_dwTsProbe = current + this.m_dwProbeWait
				this.m_dwProbe |= IKCP_ASK_SEND
			}
		}
	} else {
		this.m_dwTsProbe = 0
		this.m_dwProbeWait = 0
	}

	// flush window probing commands
	if (this.m_dwProbe & IKCP_ASK_SEND) != 0 {
		seg.m_dwCmd = IKCP_CMD_WASK
		makeSpace(IKCP_OVERHEAD)
		ptr = seg.ikcpEncodeSeg(ptr)
	}

	if (this.m_dwProbe & IKCP_ASK_TELL) != 0 {
		seg.m_dwCmd = IKCP_CMD_WINS
		makeSpace(IKCP_OVERHEAD)
		ptr = seg.ikcpEncodeSeg(ptr)
	}

	this.m_dwProbe = 0

	// calculate window size
	cwnd := imin(this.m_dwSndWnd, this.m_dwRmtWnd)
	if this.m_bNoCwnd == false {
		cwnd = imin(this.m_dwCwnd, cwnd)
	}

	// move data from snd_queue to snd_buf
	newSegsCount := 0
	for k := range this.m_stSndQueue {
		if this.m_dwSndNxt >= this.m_dwSndUna+cwnd {
			break
		}

		newseg := this.m_stSndQueue[k]
		newseg.m_dwConv = this.m_dwConv
		newseg.m_dwCmd = IKCP_CMD_PUSH
		newseg.m_dwSn = this.m_dwSndNxt
		this.m_stSndBuf = append(this.m_stSndBuf, newseg)
		this.m_dwSndNxt++
		newSegsCount++
	}

	if newSegsCount > 0 {
		this.m_stSndQueue = removeFront(this.m_stSndQueue, newSegsCount)
	}

	// calculate resent
	resent := (uint32)(this.m_iFastResend)
	if resent == 0 {
		resent = 0xffffffff
	}

	rtomin := 0
	if this.m_bNoDelay == false {
		rtomin = this.m_iRto >> 3
	}

	// flush date segments
	lost := 0
	change := 0
	current := currentMs()
	for k := range this.m_stSndBuf {
		pstSeg := &this.m_stSndBuf[k]
		if pstSeg.m_bAcked == true {
			continue
		}
		needsend := false
		_ = needsend
		if pstSeg.m_dwXmit == 0 {
			needsend = true
			pstSeg.m_dwXmit++
			pstSeg.m_dwRto = (uint32)(this.m_iRto)
			pstSeg.m_dwResendTs = current + pstSeg.m_dwRto + (uint32)(rtomin)
		} else if itimediff(current, pstSeg.m_dwResendTs) >= 0 {
			// do rto
			needsend = true
			pstSeg.m_dwXmit++
			this.m_dwXmit++
			if this.m_bNoDelay == false {
				pstSeg.m_dwRto += (uint32)(this.m_iRto)
			} else {
				pstSeg.m_dwRto += (uint32)(this.m_iRto / 2)
			}
			pstSeg.m_dwResendTs = current + pstSeg.m_dwRto
			lost++
		} else if pstSeg.m_dwFastAck >= resent {
			// do fast retransmit
			needsend = true
			pstSeg.m_dwXmit++
			pstSeg.m_dwFastAck = 0
			pstSeg.m_dwRto = (uint32)(this.m_iRto)
			pstSeg.m_dwResendTs = current + pstSeg.m_dwRto
			change++
		}

		if needsend == true {
			current = currentMs()
			pstSeg.m_dwTs = current
			pstSeg.m_dwWnd = seg.m_dwWnd
			pstSeg.m_dwUna = this.m_dwRcvNxt

			need := IKCP_OVERHEAD + len(pstSeg.m_stData)
			makeSpace(need)
			ptr = pstSeg.ikcpEncodeSeg(ptr)
			copy(ptr, pstSeg.m_stData)
			ptr = ptr[len(pstSeg.m_stData):]

			if pstSeg.m_dwXmit >= this.m_dwDeadLink {
				this.m_dwState = 0xffffffff
			}
		}
	}

	// flush remain segments
	size := len(buffer) - len(ptr)
	if size > 0 {
		this.m_stOutput(buffer, size)
	}

	// update ssthresh
	if this.m_bNoCwnd == false {
		if change > 0 {
			inflight := this.m_dwSndNxt - this.m_dwSndUna
			this.m_dwSsthresh = inflight / 2
			if this.m_dwSsthresh < IKCP_THRESH_MIN {
				this.m_dwSsthresh = IKCP_THRESH_MIN
			}
			this.m_dwCwnd = this.m_dwSsthresh + resent
			this.m_dwIncr = this.m_dwCwnd * this.m_dwMss
		}

		if lost > 0 {
			this.m_dwSsthresh = cwnd / 2
			if this.m_dwSsthresh < IKCP_THRESH_MIN {
				this.m_dwSsthresh = IKCP_THRESH_MIN
			}
			this.m_dwCwnd = 1
			this.m_dwIncr = this.m_dwMss
		}

		if this.m_dwCwnd < 1 {
			this.m_dwCwnd = 1
			this.m_dwIncr = this.m_dwMss
		}
	}
}

func (this *Ikcpcb) ikcpWndUnsed() uint16 {
	length := len(this.m_stRcvQueue)

	if length < (int)(this.m_dwRcvWnd) {
		return (uint16)((int)(this.m_dwRcvWnd) - length)
	}
	return 0
}

func (this *Ikcpcb) ikcpParseUna(una uint32) {
	count := 0
	for k := range this.m_stSndBuf {
		pstSeg := &this.m_stSndBuf[k]
		if itimediff(una, pstSeg.m_dwSn) > 0 {
			ikcpSegmentDelete(pstSeg)
			count++
		} else {
			break
		}
	}
	if count > 0 {
		this.m_stSndBuf = removeFront(this.m_stSndBuf, count)
	}
}

func (this *Ikcpcb) ikcpShrinkBuf() {
	if len(this.m_stSndBuf) > 0 {
		pstSeg := &this.m_stSndBuf[0]
		this.m_dwSndUna = pstSeg.m_dwSn
	} else {
		this.m_dwSndUna = this.m_dwSndNxt
	}
}

// predict rto & rtt
func (this *Ikcpcb) ikcpUpdateAck(rtt int) {
	if this.m_iSrtt == 0 {
		this.m_iSrtt = rtt
		this.m_iRttVal = rtt / 2
	} else {
		delta := rtt - this.m_iSrtt
		if delta < 0 {
			delta = -delta
		}

		this.m_iRttVal = (3*this.m_iRttVal + delta) / 4 // Exponential Smoothing
		this.m_iSrtt = (7*this.m_iSrtt + rtt) / 8       // Exponential Smoothing
		if this.m_iSrtt < 1 {
			this.m_iSrtt = 1
		}
	}

	rto := (uint32)(this.m_iSrtt) + imax(this.m_dwInterval, (uint32)(4*this.m_iRttVal))
	this.m_iRto = (int)(ibound((uint32)(this.m_iMinRto), rto, IKCP_RTO_MAX))

}

func (this *Ikcpcb) ikcpParseAck(sn uint32) {
	if itimediff(sn, this.m_dwSndUna) < 0 || itimediff(sn, this.m_dwSndNxt) >= 0 {
		return
	}

	for k := range this.m_stSndBuf {
		pstSeg := &this.m_stSndBuf[k]
		if sn == pstSeg.m_dwSn {
			pstSeg.m_bAcked = true
			ikcpSegmentDelete(pstSeg)
			break
		}

		if itimediff(sn, pstSeg.m_dwSn) < 0 {
			break
		}
	}
}

func (this *Ikcpcb) ikcpParseFastAck(sn uint32) {
	if itimediff(sn, this.m_dwSndUna) < 0 || itimediff(sn, this.m_dwSndNxt) >= 0 {
		return
	}

	for k := range this.m_stSndBuf {
		pstSeg := &this.m_stSndBuf[k]
		if itimediff(sn, pstSeg.m_dwSn) < 0 {
			break
		} else if sn != pstSeg.m_dwSn {
			pstSeg.m_dwFastAck++
		}
	}
}

func (this *Ikcpcb) ikcpAckPush(sn uint32, ts uint32) {
	this.m_stAckList = append(this.m_stAckList, ackItem{sn, ts})
}

func (this *Ikcpcb) ikcpParseData(newSeg ikcpseg) {
	sn := newSeg.m_dwSn
	if itimediff(sn, (this.m_dwRcvNxt+this.m_dwRcvWnd)) >= 0 ||
		itimediff(sn, this.m_dwRcvWnd) < 0 {
		return
	}

	endIndex := len(this.m_stRcvBuf) - 1
	insertIndex := 0
	repeat := false
	for i := endIndex; i >= 0; i-- {
		pstSeg := &this.m_stRcvBuf[i]
		if pstSeg.m_dwSn == sn {
			repeat = true
			break
		}

		if itimediff(sn, pstSeg.m_dwSn) > 0 {
			insertIndex = i + 1
			break
		}
	}

	if !repeat {
		// new segment
		dataCopy := make([]byte, len(newSeg.m_stData))
		copy(dataCopy, newSeg.m_stData)
		newSeg.m_stData = dataCopy
		if insertIndex == endIndex+1 {
			// insert at end
			this.m_stRcvBuf = append(this.m_stRcvBuf, newSeg)
		} else {
			// insert at mid
			this.m_stRcvBuf = append(this.m_stRcvBuf, ikcpseg{})
			copy(this.m_stRcvBuf[insertIndex+1:], this.m_stRcvBuf[insertIndex:])
			this.m_stRcvBuf[insertIndex] = newSeg
		}
	}

	// move available data from rcv_buf -> rcv_queue
	count := 0
	for k := range this.m_stRcvBuf {
		pstSeg := &this.m_stRcvBuf[k]
		if pstSeg.m_dwSn == this.m_dwRcvNxt && len(this.m_stRcvQueue) < (int)(this.m_dwRcvWnd) {
			this.m_dwRcvNxt++
			count++
		} else {
			break
		}
	}

	if count > 0 {
		this.m_stRcvQueue = append(this.m_stRcvQueue, this.m_stRcvBuf[:count]...)
		this.m_stRcvBuf = removeFront(this.m_stRcvBuf, count)
	}
}
