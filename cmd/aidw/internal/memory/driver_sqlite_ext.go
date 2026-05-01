//go:build sqlite_ext

package memory

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/mattn/go-sqlite3"
)

const driverName = "sqlite3_with_vec"

var registerOnce sync.Once

func registerDriver(home string) {
	registerOnce.Do(func() {
		sql.Register(driverName, &sqlite3.SQLiteDriver{
			ConnectHook: func(conn *sqlite3.SQLiteConn) error {
				libName := "vec0.dylib"
				if runtime.GOOS == "linux" {
					libName = "vec0.so"
				}

				libPath := filepath.Join(home, ".claude", "lib", libName)
				if _, err := os.Stat(libPath); err != nil {
					return nil
				}

				return conn.LoadExtension(libPath, "sqlite3_vec_init")
			},
		})
	})
}
