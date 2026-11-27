package gguf

import "cmp"

var swaPatterns = map[string]uint64{"gemma2": 2, "gemma3": 6, "gemma3n": 5, "gemma-embedding": 6, "cohere2": 4, "exaone4": 4, "gpt-oss": 2}

func (i *Info) KVBytes(ctx uint64, bytesPerElem float64) int64 {
	keyLen := i.KeyLen
	if keyLen == 0 && i.Heads > 0 {
		keyLen = i.EmbedDim / i.Heads
	}
	valueLen := cmp.Or(i.ValueLen, keyLen)
	if i.MLA {
		valueLen = 0
	}
	var elems uint64
	for l, heads := range i.kvHeads {
		tokens := ctx
		if l < len(i.swa) && i.swa[l] {
			tokens = min(ctx, i.Window)
		}
		elems += heads * (keyLen + valueLen) * tokens
	}
	return int64(float64(elems) * bytesPerElem)
}

func perLayer(values []uint64, fallback, blocks uint64) []uint64 {
	out := make([]uint64, blocks)
	for l := range out {
		out[l] = fallback
		if l < len(values) {
			out[l] = values[l]
		}
	}
	return out
}

func swaLayers(arch string, blocks, window uint64, flags []uint64, pattern uint64, hasPattern bool) []bool {
	if window == 0 {
		return nil
	}
	out := make([]bool, blocks)
	if len(flags) > 0 {
		for l := range out {
			out[l] = l < len(flags) && flags[l] != 0
		}
		return out
	}
	if !hasPattern {
		if pattern, hasPattern = swaPatterns[arch]; !hasPattern {
			return nil
		}
	}
	for l := range out {
		out[l] = pattern == 0 || uint64(l)%pattern < pattern-1
	}
	return out
}
