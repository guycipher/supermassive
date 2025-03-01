// BSD 3-Clause License
//
// (C) Copyright 2025, Alex Gaetano Padula & SuperMassive authors
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are met:
//
//  1. Redistributions of source code must retain the above copyright notice, this
//     list of conditions and the following disclaimer.
//
//  2. Redistributions in binary form must reproduce the above copyright notice,
//     this list of conditions and the following disclaimer in the documentation
//     and/or other materials provided with the distribution.
//
//  3. Neither the name of the copyright holder nor the names of its
//     contributors may be used to endorse or promote products derived from
//     this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
// AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
// DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
// SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
// CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
// OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
package hashtable

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// Entry is a key-value pair in the hash table
type Entry struct {
	Key       string      // The key witin the entry
	Value     interface{} // The value within the entry
	Timestamp time.Time   // The timestamp of the entry
	PSL       uint32      // Probe sequence length
}

// FilterFunc is a function type for filtering entries
type FilterFunc func(entry Entry) bool

// HashTable implements Robin Hood hashing with dynamic resizing
type HashTable struct {
	buckets []Entry // Bucket entries containing key-value pairs
	size    uint32  // Number of buckets
	used    uint32  // Number of used buckets
	// Growth and shrink thresholds
	growThreshold   float64 // Threshold to grow the table
	shrinkThreshold float64 // Threshold to shrink the table
}

// Hashtable is not thread-safe**

// New creates a new hash table with default size and thresholds
func New() *HashTable {
	return NewWithOptions(16, 0.75, 0.25)
}

// NewWithOptions creates a new hash table with custom parameters
func NewWithOptions(initialSize uint32, growThreshold, shrinkThreshold float64) *HashTable {
	return &HashTable{
		buckets:         make([]Entry, initialSize),
		size:            initialSize,
		growThreshold:   growThreshold,
		shrinkThreshold: shrinkThreshold,
	}
}

// hash generates a hash for the given key
// we use MurmurHash3 as it is fast and has good distribution..
func (ht *HashTable) hash(key string) uint32 {
	h := MurmurHash3([]byte(key), 0)
	return h % ht.size
}

// resize grows or shrinks the hash table
func (ht *HashTable) resize(newSize uint32) {
	oldBuckets := ht.buckets
	ht.buckets = make([]Entry, newSize)
	ht.size = newSize
	ht.used = 0

	// Reinsert all existing entries
	for _, entry := range oldBuckets {
		if entry.Key != "" { // Skip empty buckets
			ht.Put(entry.Key, entry.Value)
		}
	}
}

// shouldGrow checks if the table needs to grow
func (ht *HashTable) shouldGrow() bool {
	return float64(ht.used)/float64(ht.size) >= ht.growThreshold
}

// shouldShrink checks if the table needs to shrink
func (ht *HashTable) shouldShrink() bool {
	return ht.size > 16 && float64(ht.used)/float64(ht.size) <= ht.shrinkThreshold
}

// Put inserts or updates a key-value pair in the hash table
func (ht *HashTable) Put(key string, value interface{}) bool {
	// Check if we need to grow the table
	if ht.shouldGrow() {
		ht.resize(ht.size * 2) // Double the size
	}

	// Initialize the entry
	entry := Entry{
		Key:       key,
		Value:     value,
		Timestamp: time.Now(),
		PSL:       0,
	}

	index := ht.hash(key)
	for {
		// If bucket is empty
		if ht.buckets[index].Key == "" {
			ht.buckets[index] = entry
			ht.used++
			return true
		}

		// If key already exists, update value
		if ht.buckets[index].Key == key {
			ht.buckets[index].Value = value
			return true
		}

		// We use robin hood hashing, thus if current entry has lower PSL, swap
		if entry.PSL > ht.buckets[index].PSL {
			entry, ht.buckets[index] = ht.buckets[index], entry
		}

		// Move to next bucket
		entry.PSL++
		index = (index + 1) % ht.size
	}
}

// Get retrieves a value from the hash table
func (ht *HashTable) Get(key string) (interface{}, time.Time, bool) {
	index := ht.hash(key)
	probeLength := uint32(0)

	for {
		// If bucket is empty or we've probed too far
		if ht.buckets[index].Key == "" || probeLength > ht.buckets[index].PSL {
			return nil, time.Now(), false
		}

		// If we found the key
		if ht.buckets[index].Key == key {
			return ht.buckets[index].Value, ht.buckets[index].Timestamp, true
		}

		// Move to next bucket
		probeLength++
		index = (index + 1) % ht.size
	}
}

// Delete removes a key-value pair from the hash table
func (ht *HashTable) Delete(key string) bool {
	index := ht.hash(key)
	probeLength := uint32(0)

	for {
		// If bucket is empty or we've probed too far
		if ht.buckets[index].Key == "" || probeLength > ht.buckets[index].PSL {
			return false
		}

		// If we found the key
		if ht.buckets[index].Key == key {
			// Backward-shift deletion
			nextIndex := (index + 1) % ht.size
			for ht.buckets[nextIndex].Key != "" && ht.buckets[nextIndex].PSL > 0 {
				ht.buckets[index] = ht.buckets[nextIndex]
				ht.buckets[index].PSL--
				index = nextIndex
				nextIndex = (nextIndex + 1) % ht.size
			}
			ht.buckets[index] = Entry{} // Clear the last bucket
			ht.used--

			// Check if we need to shrink the table
			if ht.shouldShrink() {
				ht.resize(ht.size / 2)
			}
			return true
		}

		// Move to next bucket
		probeLength++
		index = (index + 1) % ht.size
	}
}

// Size returns the current number of entries in the hash table
func (ht *HashTable) Size() uint32 {
	return ht.used
}

// Capacity returns the current capacity of the hash table
func (ht *HashTable) Capacity() uint32 {
	return ht.size
}

// Traverse returns all entries that match the given filter function
func (ht *HashTable) Traverse(filter FilterFunc) []Entry {
	// Pre-allocate slice with a reasonable initial capacity
	results := make([]Entry, 0, ht.used)

	// Iterate through all buckets
	for _, entry := range ht.buckets {
		// Skip empty buckets
		if entry.Key == "" {
			continue
		}

		// Apply filter and collect matching entries
		if filter == nil || filter(entry) {
			results = append(results, entry)
		}
	}

	return results
}

// Incr increments the value of a key by the given increment value
func (ht *HashTable) Incr(key string, incrValue interface{}) (string, time.Time, error) {

	// We check if the value is an integer
	intVal, intErr := strconv.ParseInt(incrValue.(string), 10, 64)
	if intErr != nil {
		// We parse the value as a float
		floatVal, floatErr := strconv.ParseFloat(incrValue.(string), 64)
		if floatErr != nil {
			return "", time.Now(), fmt.Errorf("invalid value")
		}

		// We get the original value from storage
		value, ts, ok := ht.Get(key)
		if !ok {
			return "", time.Now(), fmt.Errorf("key not found")
		}

		// We convert the original value to float
		floatValOriginal, err := strconv.ParseFloat(value.(string), 64)
		if err != nil {
			return "", time.Now(), fmt.Errorf("invalid value")
		}

		floatValOriginal += floatVal

		// Store the result back with the original precision
		ht.Put(key, strconv.FormatFloat(floatValOriginal, 'f', -1, 64))

		return strconv.FormatFloat(floatValOriginal, 'f', -1, 64), ts, nil
	} else {
		// We get the original value from storage
		value, ts, ok := ht.Get(key)
		if !ok {
			return "", time.Now(), fmt.Errorf("key not found")
		}

		// We convert the original value to integer
		intValOriginal, intErr := strconv.ParseInt(value.(string), 10, 64)
		if intErr != nil {
			return "", time.Now(), fmt.Errorf("invalid value")
		}

		intValOriginal += intVal
		ht.Put(key, fmt.Sprintf("%d", intValOriginal))

		return fmt.Sprintf("%d", intValOriginal), ts, nil
	}
}

// Decr decrements the value of a key by the given decrement value
func (ht *HashTable) Decr(key string, incrValue interface{}) (string, time.Time, error) {

	// We check if the value is an integer
	intVal, intErr := strconv.ParseInt(incrValue.(string), 10, 64)
	if intErr != nil {
		// We parse the value as a float
		floatVal, floatErr := strconv.ParseFloat(incrValue.(string), 64)
		if floatErr != nil {
			return "", time.Now(), fmt.Errorf("invalid value")
		}

		// We get the original value from storage
		value, ts, ok := ht.Get(key)
		if !ok {
			return "", time.Now(), fmt.Errorf("key not found")
		}

		// We convert the original value to float
		floatValOriginal, err := strconv.ParseFloat(value.(string), 64)
		if err != nil {
			return "", time.Now(), fmt.Errorf("invalid value")
		}

		floatValOriginal -= floatVal

		if floatValOriginal < 0 {
			return "", time.Now(), fmt.Errorf("negative value")
		}

		// Store the result back with the original precision
		ht.Put(key, strconv.FormatFloat(floatValOriginal, 'f', -1, 64))

		return strconv.FormatFloat(floatValOriginal, 'f', -1, 64), ts, nil
	} else {
		// We get the original value from storage
		value, ts, ok := ht.Get(key)
		if !ok {
			return "", time.Now(), fmt.Errorf("key not found")
		}

		// We convert the original value to integer
		intValOriginal, intErr := strconv.ParseInt(value.(string), 10, 64)
		if intErr != nil {
			return "", time.Now(), fmt.Errorf("invalid value")
		}

		intValOriginal -= intVal

		if intValOriginal < 0 {
			return "", time.Now(), fmt.Errorf("negative value")
		}

		ht.Put(key, fmt.Sprintf("%d", intValOriginal))

		return fmt.Sprintf("%d", intValOriginal), ts, nil
	}

}

// GetWithRegex returns all entries whose keys match the given regex pattern
func (ht *HashTable) GetWithRegex(pattern string, limit, offset *int) ([]Entry, error) {
	// Compile the regex pattern
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	// Pre-alloc'd slice with a reasonable initial capacity
	results := make([]Entry, 0, ht.used)

	// Initialize counters
	offsetCounter := 0
	limitCounter := 0

	// Iterate through all buckets
	for _, entry := range ht.buckets {
		// Skip empty buckets
		if entry.Key == "" {
			continue
		}

		// Check if the key matches the regex pattern
		if re.MatchString(entry.Key) {
			// Apply offset if provided
			if offset != nil && offsetCounter < *offset {
				offsetCounter++
				continue
			}

			// Apply limit if provided
			if limit != nil && limitCounter < *limit {
				results = append(results, entry)
				limitCounter++
			} else if limit == nil {
				results = append(results, entry)
			}

			// Break if limit is reached
			if limit != nil && limitCounter == *limit {
				break
			}
		}
	}

	return results, nil
}

// Stats returns detailed statistics about the hash table
func (ht *HashTable) Stats() map[string]string {
	stats := make(map[string]string)

	// Basic metrics
	stats["size"] = fmt.Sprintf("%d", ht.size)
	stats["used"] = fmt.Sprintf("%d", ht.used)
	stats["load_factor"] = fmt.Sprintf("%.4f", float64(ht.used)/float64(ht.size))

	// Thresholds
	stats["grow_threshold"] = fmt.Sprintf("%.4f", ht.growThreshold)
	stats["shrink_threshold"] = fmt.Sprintf("%.4f", ht.shrinkThreshold)

	// Calculate PSL statistics
	var totalPSL uint32
	var maxPSL uint32
	emptyBuckets := uint32(0)

	for _, entry := range ht.buckets {
		if entry.Key == "" {
			emptyBuckets++
			continue
		}
		totalPSL += entry.PSL
		if entry.PSL > maxPSL {
			maxPSL = entry.PSL
		}
	}

	// PSL metrics
	stats["avg_probe_length"] = fmt.Sprintf("%.4f", float64(totalPSL)/float64(ht.used))
	stats["max_probe_length"] = fmt.Sprintf("%d", maxPSL)

	// Space efficiency
	stats["empty_buckets"] = fmt.Sprintf("%d", emptyBuckets)
	stats["empty_bucket_ratio"] = fmt.Sprintf("%.4f", float64(emptyBuckets)/float64(ht.size))
	stats["utilization"] = fmt.Sprintf("%.4f", float64(ht.used)/float64(ht.size))

	// State indicators
	stats["needs_grow"] = fmt.Sprintf("%t", ht.shouldGrow())
	stats["needs_shrink"] = fmt.Sprintf("%t", ht.shouldShrink())

	return stats
}
