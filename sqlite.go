package ephemeral

/*
#cgo linux LDFLAGS: -l:libsqlite3.so.0
#cgo darwin LDFLAGS: -lsqlite3
#include <stdlib.h>
typedef struct sqlite3 sqlite3;
typedef struct sqlite3_stmt sqlite3_stmt;
extern int sqlite3_open(const char*,sqlite3**);
extern int sqlite3_close(sqlite3*);
extern int sqlite3_busy_timeout(sqlite3*,int);
extern const char* sqlite3_errmsg(sqlite3*);
extern int sqlite3_prepare_v2(sqlite3*,const char*,int,sqlite3_stmt**,const char**);
extern int sqlite3_bind_text(sqlite3_stmt*,int,const char*,int,void(*)(void*));
extern int sqlite3_step(sqlite3_stmt*);
extern int sqlite3_finalize(sqlite3_stmt*);
extern int sqlite3_column_count(sqlite3_stmt*);
extern const unsigned char* sqlite3_column_text(sqlite3_stmt*,int);
extern int sqlite3_column_bytes(sqlite3_stmt*,int);
static int bind_text(sqlite3_stmt *s,int i,const char *v,int n){
 return sqlite3_bind_text(s,i,v,n,(void(*)(void*))-1);
}
*/
import "C"
import (
	"fmt"
	"sync"
	"unsafe"
)

type Store struct {
	mu sync.Mutex
	db *C.sqlite3
}

func Open(path string) (*Store, error) {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	s := new(Store)
	if C.sqlite3_open(p, &s.db) != 0 {
		e := s.err()
		s.Close()
		return nil, e
	}
	C.sqlite3_busy_timeout(s.db, 5000)
	for _, q := range schema {
		if _, e := s.query(q); e != nil {
			s.Close()
			return nil, e
		}
	}
	return s, nil
}
func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		C.sqlite3_close(s.db)
		s.db = nil
	}
}
func (s *Store) err() error { return fmt.Errorf("sqlite: %s", C.GoString(C.sqlite3_errmsg(s.db))) }
func (s *Store) query(sql string, args ...any) (rows, error) {
	if s.db == nil {
		return nil, fmt.Errorf("closed store")
	}
	p := C.CString(sql)
	defer C.free(unsafe.Pointer(p))
	var st *C.sqlite3_stmt
	if C.sqlite3_prepare_v2(s.db, p, -1, &st, nil) != 0 {
		return nil, s.err()
	}
	defer C.sqlite3_finalize(st)
	for i, a := range args {
		v := fmt.Sprint(a)
		b := C.CString(v)
		rc := C.bind_text(st, C.int(i+1), b, C.int(len(v)))
		C.free(unsafe.Pointer(b))
		if rc != 0 {
			return nil, s.err()
		}
	}
	out := rows{}
	for {
		switch C.sqlite3_step(st) {
		case 101:
			return out, nil
		case 100:
			row := make([]string, int(C.sqlite3_column_count(st)))
			for i := range row {
				idx := C.int(i)
				row[i] = C.GoStringN((*C.char)(unsafe.Pointer(C.sqlite3_column_text(st, idx))), C.sqlite3_column_bytes(st, idx))
			}
			out = append(out, row)
		default:
			return nil, s.err()
		}
	}
}
