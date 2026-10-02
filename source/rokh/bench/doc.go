// Package bench holds the measurements behind the 0.9.0 performance work:
// a ledger of N events built through the daemon's own commit point, then
// reopen, status, log, receipts and a further write at that size; and the
// transport path — what one peer may receive, and how fast a receiving door
// ingests pre-signed events. They are skipped unless ROKH_BENCH is set, and
// they change nothing outside their own temporary carriers.
//
//	ROKH_BENCH=1 N=10000 go test -run 'TestBench|TestTransportBench' -v ./bench/
package bench
