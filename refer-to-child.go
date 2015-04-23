package main

import (
	"bytes"
	"crypto/sha1"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type result struct {
	parent, child []byte
}

func referToChildInParent(parent, child string, shaPrefixLength int, begin, end uint64, extraHeader string, ch chan *result) {
	// Find a place to insert the custom header, if we need to.
	idx := strings.Index(parent, "committer ")
	parentBeforeCommitterField := parent[:idx]
	parentAfterCommitterField := parent[idx:len(parent)]
	h := sha1.New()
	f := fmt.Sprintf("%%0%dx", shaPrefixLength)
	for ; begin < end; begin++ {
		var parentExtra string
		if extraHeader != "" {
			// Add custom field
			parentExtra = fmt.Sprintf("%s %x\n", extraHeader, begin)
		}
		// Generate a SHA1 prefix of the desired length
		childSha1Prefix := fmt.Sprintf(f, begin%(1<<(uint(shaPrefixLength)*4)))
		// Update parent commit message to use that prefix.
		newParentAfterCommitterField := strings.Replace(
			parentAfterCommitterField, "${CHILD_SHA1}", childSha1Prefix, -1)

		// Compute parent SHA1
		parentHeader := fmt.Sprintf("commit %d\x00",
			len(parentBeforeCommitterField)+len(parentExtra)+len(newParentAfterCommitterField))
		h.Reset()
		io.WriteString(h, parentHeader)
		io.WriteString(h, parentBeforeCommitterField)
		io.WriteString(h, parentExtra)
		io.WriteString(h, newParentAfterCommitterField)
		parentSum := h.Sum(nil)
		parentSha1 := fmt.Sprintf("%40x", parentSum)

		// Update child's parent to new parent SHA1
		newChild := replaceParent(child, parentSha1)

		// Compute child SHA1
		h.Reset()
		io.WriteString(h, fmt.Sprintf("commit %d\x00", len(newChild)))
		io.WriteString(h, newChild)
		childSum := h.Sum(nil)
		childSha1 := fmt.Sprintf("%40x", childSum)

		// Check if child SHA1 matches the desired prefix
		if childSha1[:len(childSha1Prefix)] == childSha1Prefix {
			ch <- &result{
				[]byte(parentBeforeCommitterField + parentExtra + newParentAfterCommitterField),
				[]byte(newChild)}
		}
	}
	ch <- nil
}

func replaceParent(commit, parentSha string) string {
	prefix := "parent "
	sha1Start := strings.Index(commit, prefix) + len(prefix)
	sha1End := sha1Start + strings.Index(commit[sha1Start:], "\n")
	return commit[:sha1Start] + parentSha + commit[sha1End:]
}

func hardReset(sha1 string) error {
	data, err := exec.Command("git", "reset", "--hard", sha1).CombinedOutput()
	if err != nil {
		return fmt.Errorf("'git reset' failed: %s", data)
	}
	return nil
}

func writeCommit(data []byte) (string, error) {
	cmd := exec.Command("git", "hash-object", "-t", "commit", "--stdin", "-w")
	cmd.Stdin = bytes.NewReader(data)
	data, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("'git hash-object' failed: %s", data)
	}
	return string(data), nil
}

func getCommit(sha1 string) ([]byte, error) {
	data, err := exec.Command("git", "show", "--pretty=raw", sha1).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("'git show' failed: %s", data)
	}
	lines := bytes.Split(data, []byte("\n"))
	out := new(bytes.Buffer)
	for _, line := range lines {
		if bytes.HasPrefix(line, []byte("commit ")) {
			continue
		}
		if bytes.HasPrefix(line, []byte("diff ")) {
			// Reached end of commit message--remove trailing blank line
			out.Truncate(out.Len() - 1)
			break
		}
		if bytes.HasPrefix(line, []byte("    ")) {
			// Undo commit message indenting
			line = line[4:]
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	return out.Bytes(), nil
}

func die(err string) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

var parentSha1 = flag.String("parent", "", "sha1 of the parent commit")
var childSha1 = flag.String("child", "", "sha1 of child commit")
var sha1PrefixLength = flag.Int("prefix-length", 6, "length of SHA1 prefix to replace ${CHILD_SHA1} with")
var parallelism = flag.Int("parallelism", 8, "number of parallel searches to conduct")
var dryRun = flag.Bool("dry-run", false, "Don't actually write any objects or update HEAD; just try to find a good prefix")
var extraHeaderName = flag.String("extra-header", "", "The name of an extra header to add to the parent (see README)")

func main() {
	runtime.GOMAXPROCS(*parallelism)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		fmt.Fprintln(os.Stderr,
			"Given commit c and its parent p, update the current git repo's HEAD to"+
				" point to c', which is the same as c except that it has parent p',"+
				" which is the same as p except that ${CHILD_SHA1} is replaced with"+
				" sha1(c') whenever it occurs in p's commit message.")
		flag.PrintDefaults()
	}
	flag.Parse()
	p, err := getCommit(*parentSha1)
	if err != nil {
		die(err.Error())
	}
	c, err := getCommit(*childSha1)
	if err != nil {
		die(err.Error())
	}
	ch := make(chan *result)
	start := uint64(0)
	inc := uint64(1) << (4 * uint(*sha1PrefixLength)) / uint64(*parallelism)
	var r *result
	for {
		for i := 0; i < *parallelism; i++ {
			go referToChildInParent(string(p), string(c), *sha1PrefixLength, start, start+inc, *extraHeaderName, ch)
			start += inc
		}
		for i := 0; i < *parallelism; i++ {
			r = <-ch
			if r != nil {
				break
			}
		}
		if r == nil {
			if *extraHeaderName == "" {
				die("Did not succeed just by updating the parent message." +
					" Specify -extra-header if you would like to try adding an" +
					" extra header to the parent to give us more text to play with.")
			}
			start += 1 << (4 * uint(*sha1PrefixLength))
		} else {
			break
		}
	}

	if !*dryRun {
		// Reset to before parent
		if err = hardReset(*parentSha1 + "~1"); err != nil {
			die(err.Error())
		}
		// Write new parent and child commits
		sha1, err := writeCommit(r.parent)
		if err != nil {
			die(err.Error())
		}
		sha1, err = writeCommit(r.child)
		if err != nil {
			die(err.Error())
		}

		// Set HEAD to new child
		if err = hardReset(strings.TrimSpace(sha1)); err != nil {
			die(err.Error())
		}
	}

	os.Exit(0)
}
