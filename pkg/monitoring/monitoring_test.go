package monitoring

import (
	"os"
	"testing"
)

func TestReadNumber(t *testing.T) {
	filename := "test-number-in-file"
	writeFile(filename, "60211831846493\n")
	defer os.Remove(filename)

	t.Log("Give a file with a number 60211831846493 and LF char")
	if number, _ := ReadNumber(filename); number != 60211831846493 {
		t.Fatalf("ReadNumber should get value 60211831846493, but got %d", number)
	}
	t.Log("ReadNumber get value 60211831846493")
}

func TestReadTotalInactiveFile(t *testing.T) {
	filename := "test-memory-stat-file"
	writeFile(filename, `cache 237895680
rss 4407296
rss_huge 0
mapped_file 13365248
swap 0
pgpgin 3884506
pgpgout 3825350
pgfault 4061338
pgmajfault 84
inactive_anon 8409088
active_anon 4517888
inactive_file 116477952
active_file 112898048
unevictable 0
hierarchical_memory_limit 9223372036854771712
hierarchical_memsw_limit 9223372036854771712
total_cache 11008163840
total_rss 3624939520
total_rss_huge 853540864
total_mapped_file 824639488
total_swap 0
total_pgpgin 1569197391
total_pgpgout 1578989550
total_pgfault 3236424211
total_pgmajfault 2153
total_inactive_anon 94371840
total_active_anon 3636318208
total_inactive_file 4666023936
total_active_file 6236336128
total_unevictable 0
`)
	defer os.Remove(filename)

	t.Logf("Give a memory.stat file with total_inactive_file=4666023936")
	value, _ := ReadTotalInactiveFile(filename)
	if value != 4666023936 {
		t.Fatal("ReadTotalInactiveFile should get 4666023936")
	}
	t.Log("ReadTotalInactiveFile get 4666023936")

}

func writeFile(filename string, content string) {
	f, _ := os.Create(filename)
	defer f.Close()
	f.WriteString(content)
}
