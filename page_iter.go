//go:build go1.23

package didit

import "iter"

// All returns an iterator over the remaining items, for a range loop:
//
//	for session, err := range client.Sessions.ListAutoPaging(ctx, params).All() {
//		if err != nil {
//			return err
//		}
//		// ...
//	}
//
// An error ends the iteration after it is yielded. It needs Go 1.23 or later;
// use Next, Current and Err on Go 1.22.
func (it *PageIterator[T]) All() iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		for it.Next() {
			if !yield(it.Current(), nil) {
				return
			}
		}
		if err := it.Err(); err != nil {
			var zero T
			yield(zero, err)
		}
	}
}
