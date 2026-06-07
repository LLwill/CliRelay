package usage

type scannable interface {
	Scan(dest ...interface{}) error
}
