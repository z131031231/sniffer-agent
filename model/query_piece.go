package model

import (
	jsoniter "github.com/json-iterator/go"
	"github.com/pingcap/tidb/util/hack"
	"time"
)

//实例化工具类，jsoniter解码json
var jsonIterator = jsoniter.ConfigCompatibleWithStandardLibrary

type QueryPiece interface {
	String() *string
	Bytes() []byte
	GetSQL() *string
	NeedSyncSend() bool
	Recovery()
}

// BaseQueryPiece 基础信息
type BaseQueryPiece struct {
	SyncSend          bool    `json:"-"`
	ServerIP          *string `json:"sip"` //请求端地址
	ServerPort        int     `json:"sport"` //请求端端口
	CapturePacketRate float64 `json:"cpr"`  //抓包频率
	EventTime         int64   `json:"bt"`  //抓取时间
	jsonContent       []byte  `json:"-"`
}

const (
	millSecondUnit = int64(time.Millisecond)
)

var (
	mqpp = NewMysqlQueryPiecePool()
)

var commonBaseQueryPiece = &BaseQueryPiece{}

func NewBaseQueryPiece(
	serverIP *string, serverPort int, capturePacketRate float64) (
	bqp *BaseQueryPiece) {
	bqp = commonBaseQueryPiece
	bqp.ServerIP = serverIP
	bqp.ServerPort = serverPort
	bqp.SyncSend = false
	bqp.CapturePacketRate = capturePacketRate
	bqp.EventTime = time.Now().UnixNano() / millSecondUnit

	return
}

func (bqp *BaseQueryPiece) NeedSyncSend() (bool) {
	return bqp.SyncSend
}

func (bqp *BaseQueryPiece) SetNeedSyncSend(syncSend bool) {
	bqp.SyncSend = syncSend
}

func (bqp *BaseQueryPiece) String() (*string) {
	content := bqp.Bytes()
	contentStr := hack.String(content)
	return &contentStr
}

func (bqp *BaseQueryPiece) Bytes() (content []byte) {
	// content, err := json.Marshal(bqp)
	if bqp.jsonContent != nil && len(bqp.jsonContent) > 0 {
		return bqp.jsonContent
	}

	bqp.jsonContent = marsharQueryPieceMonopolize(bqp)
	return bqp.jsonContent
}

func (bqp *BaseQueryPiece) GetSQL() (*string) {
	return nil
}

func (bqp *BaseQueryPiece) Recovery() {
}

/**
func marsharQueryPieceShareMemory(qp interface{}, cacheBuffer []byte) []byte {
	buffer := bytes.NewBuffer(cacheBuffer)
	err := json.NewEncoder(buffer).Encode(qp)
	if err != nil {
		return []byte(err.Error())
	}

	return buffer.Bytes()
}
*/

func marsharQueryPieceMonopolize(qp interface{}) (content []byte) {
	content, err := jsonIterator.Marshal(qp)
	if err != nil {
		return []byte(err.Error())
	}

	return content
}
