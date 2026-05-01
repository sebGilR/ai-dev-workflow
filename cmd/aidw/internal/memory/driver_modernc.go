//go:build !sqlite_ext

package memory

import _ "modernc.org/sqlite"

const driverName = "sqlite"

func registerDriver(home string) {}
