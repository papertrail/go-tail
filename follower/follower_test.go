package follower

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var (
	tmpDir string

	_         = fmt.Print
	testLines = [][]string{
		{
			"’Twas brillig, and the slithy toves",
			"      Did gyre and gimble in the wabe:",
			"All mimsy were the borogoves,",
			"      And the mome raths outgrabe.",

			"“Beware the Jabberwock, my son!",
			"      The jaws that bite, the claws that catch!",
			"Beware the Jubjub bird, and shun",
			"      The frumious Bandersnatch!”",

			"He took his vorpal sword in hand;",
			"      Long time the manxome foe he sought—",
			"So rested he by the Tumtum tree",
			"      And stood awhile in thought.",

			"And, as in uffish thought he stood,",
			"      The Jabberwock, with eyes of flame,",
			"Came whiffling through the tulgey wood,",
			"      And burbled as it came!",

			"One, two! One, two! And through and through",
			"      The vorpal blade went snicker-snack!",
			"He left it dead, and with its head",
			"      He went galumphing back.",

			"“And hast thou slain the Jabberwock?",
			"      Come to my arms, my beamish boy!",
			"O frabjous day! Callooh! Callay!”",
			"      He chortled in his joy.",

			"’Twas brillig, and the slithy toves",
			"      Did gyre and gimble in the wabe:",
			"All mimsy were the borogoves,",
			"      And the mome raths outgrabe.",
		},

		{
			"The winter evening settles down",
			"With smell of steaks in passageways.",
			"Six o’clock.",
			"The burnt-out ends of smoky days.",
			"And now a gusty shower wraps",
			"The grimy scraps",
			"Of withered leaves about your feet",
			"And newspapers from vacant lots;",
			"The showers beat",
			"On broken blinds and chimney-pots,",
			"And at the corner of the street",
			"A lonely cab-horse steams and stamps.",
			"And then the lighting of the lamps.",
		},
	}
)

func TestMain(m *testing.M) {
	tmpDir, _ = ioutil.TempDir("", "fllw")
	rs := m.Run()
	os.RemoveAll(tmpDir)
	os.Exit(rs)
}

func TestNoReopen(t *testing.T) {
	file, f := testPair(t, "test1")
	defer file.Close()

	// sleep to make sure we don't write before the follower is ready
	time.Sleep(100 * time.Millisecond)
	if err := writeLines(file, testLines[0]); err != nil {
		t.Fatal(err)
	}

	assertFollowedLines(t, f, testLines[0])
}

func TestTruncate(t *testing.T) {
	file, f := testPair(t, "test2")
	defer file.Close()

	// sleep to make sure we don't write before the follower is ready
	time.Sleep(100 * time.Millisecond)
	if err := writeLines(file, testLines[0]); err != nil {
		t.Fatal(err)
	}

	assertFollowedLines(t, f, testLines[0])

	// truncate to 0 size
	if err := file.Truncate(0); err != nil {
		t.Fatal(err)
	}

	// write a different set of lines
	if err := writeLines(file, testLines[1]); err != nil {
		t.Fatal(err)
	}

	assertFollowedLines(t, f, testLines[1])
}

func TestRenameCreate(t *testing.T) {
	file, f := testPair(t, "test2")
	defer file.Close()

	// sleep to make sure we don't write before the follower is ready
	time.Sleep(100 * time.Millisecond)
	if err := writeLines(file, testLines[0]); err != nil {
		t.Fatal(err)
	}

	assertFollowedLines(t, f, testLines[0])

	oldName := file.Name()
	if err := os.Rename(oldName, oldName+".1"); err != nil {
		t.Fatal(err)
	}

	newFile, err := os.Create(oldName)
	if err != nil {
		t.Fatal(err)
	}
	defer newFile.Close()

	// write a different set of lines
	if err := writeLines(newFile, testLines[1]); err != nil {
		t.Fatal(err)
	}

	assertFollowedLines(t, f, testLines[1])
}

func testPair(t *testing.T, filename string) (*os.File, *Follower) {
	file, err := os.Create(path.Join(tmpDir, filename))
	if err != nil {
		t.Fatal(err)
	}

	f, err := New(file.Name(), Config{
		Reopen: false,
		Offset: 0,
		Whence: io.SeekEnd,
	})
	if err != nil {
		t.Fatal(err)
	}

	return file, f
}

func writeLines(file *os.File, lines []string) error {
	for _, l := range lines {
		_, err := file.WriteString(l + "\n")
		if err != nil {
			return err
		}
	}

	return nil
}

func assertFollowedLines(t *testing.T, f *Follower, lines []string) {
	assert := assert.New(t)

	wg := &sync.WaitGroup{}
	wg.Add(1)

	go func() {
		defer wg.Done()

		i := 0
		for line := range f.Lines() {
			assert.Equal(lines[i], line.String())
			i++
			if i == len(lines) {
				return
			}
		}
	}()

	wg.Wait()
}
