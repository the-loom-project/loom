package gguf

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
)

type Kind int

const (
	Chat Kind = iota
	Embedding
	NoChat
	NotLM
)

func (k Kind) String() string {
	switch k {
	case Chat:
		return "chat"
	case Embedding:
		return "embedding"
	}
	if k == NotLM {
		return "not a language model"
	}
	return "no chat template"
}

type Info struct {
	Arch      string
	Name      string
	SizeLabel string
	Context   uint64
	Blocks    uint64
	EmbedDim  uint64
	Heads     uint64
	HeadsKV   uint64
	Experts   uint64
	Params    uint64

	HasChatTemplate bool
	HasPooling      bool
	HasTokenizer    bool
}

var activeRe = regexp.MustCompile(`(?i)-A\d+(\.\d+)?[BM]$`)

func (i *Info) Size() string {
	if activeRe.MatchString(i.SizeLabel) {
		return i.SizeLabel
	}
	return Params(i.Params)
}

func Params(n uint64) string {
	switch {
	case n == 0:
		return "?"
	case n >= 10e9:
		return fmt.Sprintf("%.0fB", float64(n)/1e9)
	case n >= 1e9:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.0fM", float64(n)/1e6)
	}
	return fmt.Sprint(n)
}

func (i *Info) ContextOr(def uint64) uint64 {
	if i == nil || i.Context == 0 {
		return def
	}
	return i.Context
}

func (i *Info) Kind() Kind {
	switch {
	case !i.HasTokenizer || i.Context == 0:
		return NotLM
	case i.HasPooling:
		return Embedding
	case i.HasChatTemplate:
		return Chat
	}
	return NoChat
}

func (i *Info) KVBytesPerToken(bytesPerElem float64) float64 {
	if i.Blocks == 0 || i.Heads == 0 || i.EmbedDim == 0 {
		return 0
	}
	kv := i.HeadsKV
	if kv == 0 {
		kv = i.Heads
	}
	return 2 * float64(i.Blocks) * float64(kv) * float64(i.EmbedDim/i.Heads) * bytesPerElem
}

const (
	tU8, tI8, tU16, tI16, tU32, tI32, tF32, tBool, tString, tArray, tU64, tI64, tF64 = 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12
)

var errCorrupt = errors.New("not a valid GGUF file")

func Read(path string) (*Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	d := &decoder{r: bufio.NewReaderSize(f, 1<<20)}

	magic := make([]byte, 4)
	if _, err := io.ReadFull(d.r, magic); err != nil || string(magic) != "GGUF" {
		return nil, errCorrupt
	}
	d.u32()
	tensors := d.u64()
	count := d.u64()

	info := &Info{}
	nums := map[string]uint64{}
	for i := uint64(0); i < count && d.err == nil; i++ {
		key := d.str()
		num, str := d.value(d.u32())
		switch key {
		case "general.architecture":
			info.Arch = str
		case "general.name":
			info.Name = str
		case "general.size_label":
			info.SizeLabel = str
		case "tokenizer.chat_template":
			info.HasChatTemplate = true
		default:
			nums[key] = num
			if strings.HasPrefix(key, "tokenizer.ggml.") {
				info.HasTokenizer = true
			}
			if strings.HasSuffix(key, ".pooling_type") {
				info.HasPooling = true
			}
		}
	}
	for i := uint64(0); i < tensors && d.err == nil; i++ {
		d.str()
		dims, n := d.u32(), uint64(1)
		for j := uint32(0); j < dims && d.err == nil; j++ {
			n *= d.u64()
		}
		d.u32()
		d.u64()
		info.Params += n
	}
	if d.err != nil {
		return nil, fmt.Errorf("%s: %w", path, errors.Join(errCorrupt, d.err))
	}
	a := info.Arch + "."
	info.Context = nums[a+"context_length"]
	info.Blocks = nums[a+"block_count"]
	info.EmbedDim = nums[a+"embedding_length"]
	info.Heads = nums[a+"attention.head_count"]
	info.HeadsKV = nums[a+"attention.head_count_kv"]
	info.Experts = nums[a+"expert_count"]
	return info, nil
}

type decoder struct {
	r   *bufio.Reader
	err error
}

func (d *decoder) bytes(n int) []byte {
	if d.err != nil {
		return nil
	}
	b := make([]byte, n)
	_, d.err = io.ReadFull(d.r, b)
	return b
}

func (d *decoder) u32() uint32 {
	if b := d.bytes(4); b != nil {
		return binary.LittleEndian.Uint32(b)
	}
	return 0
}

func (d *decoder) u64() uint64 {
	if b := d.bytes(8); b != nil {
		return binary.LittleEndian.Uint64(b)
	}
	return 0
}

func (d *decoder) str() string {
	n := d.u64()
	if n > 1<<28 {
		d.err = errCorrupt
		return ""
	}
	return string(d.bytes(int(n)))
}

func (d *decoder) value(t uint32) (num uint64, str string) {
	switch t {
	case tU8, tI8, tBool:
		if b := d.bytes(1); b != nil {
			num = uint64(b[0])
		}
	case tU16, tI16:
		if b := d.bytes(2); b != nil {
			num = uint64(binary.LittleEndian.Uint16(b))
		}
	case tU32, tI32, tF32:
		num = uint64(d.u32())
	case tU64, tI64, tF64:
		num = d.u64()
	case tString:
		str = d.str()
	case tArray:
		et, n := d.u32(), d.u64()
		for i := uint64(0); i < n && d.err == nil; i++ {
			d.value(et)
		}
	default:
		d.err = errCorrupt
	}
	return
}

var quantRe = regexp.MustCompile(`(?i)(^|[-_./])((?:UD-)?(?:[IT]?Q[0-9]+_[A-Z0-9]+(?:_[A-Z0-9]+)?|BF16|F16|F32|MXFP4))`)

func QuantOf(path string) string {
	m := quantRe.FindAllStringSubmatch(strings.TrimSuffix(path, ".gguf"), -1)
	if len(m) == 0 {
		return ""
	}
	return strings.ToUpper(m[len(m)-1][2])
}

func IsMMProj(path string) bool {
	return strings.HasSuffix(path, ".gguf") && strings.Contains(strings.ToLower(path), "mmproj")
}

func BestMMProj(paths []string) string {
	best := ""
	for _, p := range paths {
		if best == "" || mmprojRank(p) > mmprojRank(best) {
			best = p
		}
	}
	return best
}

func mmprojRank(path string) int { return slices.Index([]string{"F32", "BF16", "F16"}, QuantOf(path)) }

var auxRe = regexp.MustCompile(`(?i)(^|/)(mtp[-_/]|[^/]*imatrix)`)

func IsWeights(path string) bool {
	return strings.HasSuffix(path, ".gguf") && !IsMMProj(path) && !auxRe.MatchString(path)
}

func QuantNote(name string) string {
	dynamic := ""
	if rest, ok := strings.CutPrefix(name, "UD-"); ok {
		name, dynamic = rest, " · Unsloth dynamic (mixed precision)"
	}
	var note string
	switch {
	case name == "BF16" || name == "F16" || name == "F32":
		note = "unquantized: full quality, full size"
	case strings.HasPrefix(name, "Q8"):
		note = "near-lossless"
	case strings.HasPrefix(name, "Q6"):
		note = "very high quality"
	case strings.HasPrefix(name, "Q5"):
		note = "high quality"
	case name == "Q4_K_M":
		note = "balanced: the usual pick"
	case strings.HasPrefix(name, "Q4") || strings.HasPrefix(name, "IQ4"):
		note = "good quality, smaller"
	case strings.HasPrefix(name, "Q3") || strings.HasPrefix(name, "IQ3"):
		note = "small, some quality loss"
	case strings.HasPrefix(name, "Q2") || strings.HasPrefix(name, "IQ2"):
		note = "tiny, noticeably worse"
	case strings.HasPrefix(name, "IQ1"):
		note = "extremely small, poor quality"
	case name == "MXFP4":
		note = "4-bit floating point"
	}
	return note + dynamic
}
