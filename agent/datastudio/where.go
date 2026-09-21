package datastudio

import (
	"fmt"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/model"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

// whereFromWire turns the operator filters a ListRecordsRequest carries into
// the model layer's Where clause. It refuses rather than drops: a column
// without a name or an operator the closed set does not know is an error
// the operator sees, because a filter quietly treated as "no filter"
// answers every row and looks like a result (ADR-001's rule, kept on the
// wire).
func whereFromWire(in []*adminv1.RecordFilter) ([]model.Filter, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]model.Filter, 0, len(in))
	for i, f := range in {
		if f == nil {
			continue
		}
		column := strings.TrimSpace(f.GetColumn())
		if column == "" {
			return nil, fmt.Errorf("admin agent: where[%d]: column is required", i)
		}
		op, ok := model.ParseFilterOp(f.GetOp())
		if !ok {
			return nil, fmt.Errorf("admin agent: where[%d] on %q: unknown operator %q (one of eq, ne, gt, gte, lt, lte, contains, startswith, endswith, in, not_in, isnull)", i, column, f.GetOp())
		}
		out = append(out, model.Filter{
			Column: column,
			Op:     op,
			Value:  f.GetValue(),
			Values: append([]string(nil), f.GetValues()...),
		})
	}
	return out, nil
}
