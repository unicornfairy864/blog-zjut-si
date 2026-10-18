package global

import "gorm.io/gorm"

// DB 是全进程唯一的数据库句柄；事务必须由 service 用 DB.Transaction 发起。
var DB *gorm.DB
