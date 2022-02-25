package mysql

import (
	"fmt"

	_ "github.com/go-sql-driver/mysql"
	du "github.com/zr-hebo/util-db"
)

func expandLocalMysql(port int) (mysqlHost *du.MysqlDB) {
	mysqlHost = new(du.MysqlDB)      //初始化数据库连接
	mysqlHost.Port = port            //端口
	mysqlHost.UserName = adminUser   //数据库账号
	mysqlHost.Passwd = adminPasswd   //数据库密码
	mysqlHost.DBName = "information_schema" //数据库地址
	mysqlHost.IP = adminAddr
	mysqlHost.DatabaseType = "mysql" //数据库类型
	mysqlHost.ConnectTimeout = 1     //连接超时时间


	return
}

//获取长连接的账号和数据库
func querySessionInfo(snifferPort int, clientHost *string) (user, db *string, err error) {
	mysqlServer := expandLocalMysql(snifferPort)
	querySQL := fmt.Sprintf(
		"SELECT user, db FROM information_schema.processlist WHERE host='%s'", *clientHost)
	// log.Debug(querySQL)
	queryRow, err := mysqlServer.QueryRow(querySQL)
	if err != nil {
		return
	}

	if queryRow == nil {
		return
	}

	userVal := queryRow.Record["user"]
	if userVal != nil {
		usrStr := userVal.(string)
		user = &usrStr
	}

	dbVal := queryRow.Record["db"]
	if dbVal != nil {
		dbStr := dbVal.(string)
		db = &dbStr
	}

	return
}