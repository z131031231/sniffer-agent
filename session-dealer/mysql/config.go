package mysql

import (
	"flag"
	"fmt"
	"sniffer-agent/util"
	"regexp"
)

var (
	uselessSQLPattern = regexp.MustCompile(`(?i)^\s*(select @@version_comment limit 1)`)
	ddlPatern = regexp.MustCompile(`(?i)^\s*(create|alter|drop)`)
)

var (
	strictMode bool     //执行模式:严格模式长连接通过连接数据库获取对应信息，非严格模式长连接对应信息为空的情况下不现实数据库和用户
	adminUser string    //数据库用户名
	adminPasswd string  //数据库密码
	adminAddr string   //数据库地址
	// MaxMySQLPacketLen is the max packet payload length.
	MaxMySQLPacketLen int //设置语句缓存存储的最大页数
	coverRangePool    = NewCoveragePool()
	localStmtCache *util.SliceBufferPool
	PrepareStatement = []byte(":prepare")
)

func init() {
	flag.BoolVar(&strictMode,"strict_mode", false, "strict mode. Default is false")
	flag.StringVar(&adminUser,"admin_user", "root", "admin user name. When set strict mode, must set admin user to query session info")
	flag.StringVar(&adminPasswd,"admin_passwd", "root", "admin user passwd. When use strict mode, must set admin user to query session info")
	flag.StringVar(&adminAddr,"admin_addr", "127.0.0.1", "admin addr. When use strict mode")
	flag.IntVar(&MaxMySQLPacketLen, "max_packet_length", 128 * 1024, "max mysql packet length. Default is 128 * 1024")
}

func PrepareEnv()  {
	localStmtCache = util.NewSliceBufferPool("statement cache", MaxMySQLPacketLen)
}

func CheckParams()  {
	if !strictMode {
		return
	}

	if len(adminUser) < 1 {
		panic(fmt.Sprintf("In strict mode, admin user name cannot be empty"))
	}
	fmt.Println(adminPasswd)

	if len(adminPasswd) < 1 {
		panic(fmt.Sprintf("In strict mode, admin passwd cannot be empty"))
	}

	if len(adminAddr) < 1 {
		panic(fmt.Sprintf("In strict mode, admin addr cannot be empty"))
	}
}
