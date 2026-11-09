//go:build go1.23

package checkslip

import "iter"

// All returns the remaining items as an iterator for range loops. The loop
// gets a non-nil error at most once, as its last value; stop on it.
//
//	for rec, err := range client.Slips.ListAll(ctx, nil).All() {
//		if err != nil {
//			return err
//		}
//		fmt.Println(rec.RequestID)
//	}
//
// All needs Go 1.23 or later. On Go 1.22 use Next, Current and Err.
func (it *Iter[T]) All() iter.Seq2[T, error] {
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
