package tushare

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// Table 是 tushare 接口返回的结果集，对应 Python SDK 里的 pandas.DataFrame。
//
// 服务端返回的是「列名 + 二维数组」结构，Table 原样保留这套结构以便零损耗转换，
// 同时提供 Record / Scan / Column 等便捷访问方式。
//
// 数值单元格的类型约定：整数值为 int64，带小数或科学计数法为 float64，
// 字符串为 string，缺失值为 nil。这样既避免了 float64 的精度丢失，
// 也让 fmt.Println 输出更接近原始 JSON。
type Table struct {
	// API 是产生该结果集的 api_name。
	API string
	// Fields 是列名，顺序与服务端返回一致。
	Fields []string
	// Items 是行数据，Items[i][j] 对应 Fields[j]。
	Items [][]any
	// HasMore 表示服务端还有未返回的数据（配合 offset/limit 分页）。
	HasMore bool
}

// Len 返回行数。
func (t *Table) Len() int {
	if t == nil {
		return 0
	}
	return len(t.Items)
}

// IsEmpty 返回结果集是否为空。
func (t *Table) IsEmpty() bool { return t.Len() == 0 }

// FieldIndex 返回列下标。
func (t *Table) FieldIndex(name string) (int, bool) {
	if t == nil {
		return 0, false
	}
	for i, f := range t.Fields {
		if f == name {
			return i, true
		}
	}
	// 宽松匹配：忽略大小写与下划线差异。
	key := normalizeKey(name)
	for i, f := range t.Fields {
		if normalizeKey(f) == key {
			return i, true
		}
	}
	return 0, false
}

// HasField 判断列是否存在。
func (t *Table) HasField(name string) bool {
	_, ok := t.FieldIndex(name)
	return ok
}

// Row 返回第 i 行，越界返回空行。
func (t *Table) Row(i int) Row {
	if t == nil || i < 0 || i >= len(t.Items) {
		return Row{fields: nil, values: nil}
	}
	return Row{fields: t.Fields, values: t.Items[i]}
}

// Rows 返回行迭代器，可直接用于 range-over-func（Go 1.23+）。
//
//	for row := range tbl.Rows() {
//		fmt.Println(row.Str("ts_code"), row.Float("close"))
//	}
func (t *Table) Rows() func(yield func(Row) bool) {
	return func(yield func(Row) bool) {
		if t == nil {
			return
		}
		for i := range t.Items {
			if !yield(Row{fields: t.Fields, values: t.Items[i]}) {
				return
			}
		}
	}
}

// Records 把结果集转成 []map[string]any，便于序列化或二次处理。
func (t *Table) Records() []map[string]any {
	if t == nil {
		return nil
	}
	out := make([]map[string]any, 0, len(t.Items))
	for i := range t.Items {
		out = append(out, t.rowMap(i))
	}
	return out
}

func (t *Table) rowMap(i int) map[string]any {
	m := make(map[string]any, len(t.Fields))
	row := t.Items[i]
	for j, f := range t.Fields {
		if j < len(row) {
			m[f] = row[j]
		}
	}
	return m
}

// Column 返回名为 name 的一列（原始类型）。
func (t *Table) Column(name string) ([]any, bool) {
	idx, ok := t.FieldIndex(name)
	if !ok {
		return nil, false
	}
	out := make([]any, len(t.Items))
	for i, row := range t.Items {
		if idx < len(row) {
			out[i] = row[idx]
		}
	}
	return out, true
}

// Floats 返回 float64 列，缺失值填 0；列不存在返回错误。
func (t *Table) Floats(name string) ([]float64, error) {
	idx, ok := t.FieldIndex(name)
	if !ok {
		return nil, fmt.Errorf("tushare: 列 %q 不存在", name)
	}
	out := make([]float64, 0, len(t.Items))
	for _, row := range t.Items {
		var v float64
		if idx < len(row) {
			if f, err := toFloat(row[idx]); err == nil {
				v = f
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// Strings 返回 string 列。
func (t *Table) Strings(name string) ([]string, error) {
	idx, ok := t.FieldIndex(name)
	if !ok {
		return nil, fmt.Errorf("tushare: 列 %q 不存在", name)
	}
	out := make([]string, 0, len(t.Items))
	for _, row := range t.Items {
		var v string
		if idx < len(row) && row[idx] != nil {
			v = toString(row[idx])
		}
		out = append(out, v)
	}
	return out, nil
}

// Select 返回只包含指定列的新结果集（类似 df[['a','b']]）。
func (t *Table) Select(fields ...string) (*Table, error) {
	if t == nil {
		return nil, fmt.Errorf("tushare: 结果集为空指针")
	}
	if len(fields) == 0 {
		return t.Clone(), nil
	}
	idxs := make([]int, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		i, ok := t.FieldIndex(f)
		if !ok {
			return nil, fmt.Errorf("tushare: 列 %q 不存在", f)
		}
		idxs = append(idxs, i)
	}
	out := &Table{API: t.API, HasMore: t.HasMore, Fields: make([]string, 0, len(idxs))}
	for _, i := range idxs {
		out.Fields = append(out.Fields, t.Fields[i])
	}
	out.Items = make([][]any, len(t.Items))
	for r, row := range t.Items {
		dst := make([]any, len(idxs))
		for c, i := range idxs {
			if i < len(row) {
				dst[c] = row[i]
			}
		}
		out.Items[r] = dst
	}
	return out, nil
}

// Head 返回前 n 行。
func (t *Table) Head(n int) *Table { return t.slice(0, n) }

// Tail 返回后 n 行。
func (t *Table) Tail(n int) *Table {
	if t == nil {
		return nil
	}
	if n > t.Len() {
		n = t.Len()
	}
	return t.slice(t.Len()-n, t.Len())
}

func (t *Table) slice(from, to int) *Table {
	if t == nil {
		return nil
	}
	if from < 0 {
		from = 0
	}
	if to > t.Len() {
		to = t.Len()
	}
	if from > to {
		from = to
	}
	out := &Table{API: t.API, HasMore: t.HasMore, Fields: append([]string(nil), t.Fields...)}
	out.Items = make([][]any, 0, to-from)
	for _, row := range t.Items[from:to] {
		out.Items = append(out.Items, append([]any(nil), row...))
	}
	return out
}

// Clone 深拷贝结果集。
func (t *Table) Clone() *Table {
	if t == nil {
		return nil
	}
	return t.slice(0, t.Len())
}

// AddColumn 追加或覆盖一列；values 长度不足时以 nil 补齐。
func (t *Table) AddColumn(name string, values []any) {
	if t == nil {
		return
	}
	if i, ok := t.FieldIndex(name); ok {
		for r := range t.Items {
			var v any
			if r < len(values) {
				v = values[r]
			}
			t.Items[r][i] = v
		}
		return
	}
	t.Fields = append(t.Fields, name)
	for r := range t.Items {
		var v any
		if r < len(values) {
			v = values[r]
		}
		t.Items[r] = append(t.Items[r], v)
	}
}

// DropColumns 删除指定列（不存在则忽略）。
func (t *Table) DropColumns(names ...string) {
	if t == nil || len(names) == 0 {
		return
	}
	drop := make(map[string]bool, len(names))
	for _, n := range names {
		drop[normalizeKey(n)] = true
	}
	keep := make([]int, 0, len(t.Fields))
	for i, f := range t.Fields {
		if !drop[normalizeKey(f)] {
			keep = append(keep, i)
		}
	}
	if len(keep) == len(t.Fields) {
		return
	}
	fields := make([]string, len(keep))
	for i, idx := range keep {
		fields[i] = t.Fields[idx]
	}
	for r, row := range t.Items {
		dst := make([]any, len(keep))
		for c, idx := range keep {
			if idx < len(row) {
				dst[c] = row[idx]
			}
		}
		t.Items[r] = dst
	}
	t.Fields = fields
}

// SortBy 按指定列原地排序；desc 为 true 时降序。
//
// 仅支持 string / int64 / float64 单元格，其余类型视为最小。
func (t *Table) SortBy(field string, desc bool) error {
	idx, ok := t.FieldIndex(field)
	if !ok {
		return fmt.Errorf("tushare: 列 %q 不存在", field)
	}
	sort.SliceStable(t.Items, func(a, b int) bool {
		c := compareCell(cellAt(t.Items[a], idx), cellAt(t.Items[b], idx))
		if desc {
			return c > 0
		}
		return c < 0
	})
	return nil
}

func cellAt(row []any, i int) any {
	if i < len(row) {
		return row[i]
	}
	return nil
}

func compareCell(a, b any) int {
	af, aok := a.(float64)
	bf, bok := b.(float64)
	if !aok {
		if ai, ok := a.(int64); ok {
			af, aok = float64(ai), true
		}
	}
	if !bok {
		if bi, ok := b.(int64); ok {
			bf, bok = float64(bi), true
		}
	}
	if aok && bok {
		switch {
		case af < bf:
			return -1
		case af > bf:
			return 1
		default:
			return 0
		}
	}
	return strings.Compare(toString(a), toString(b))
}

// Scan 按列名把结果集写入 dest。
//
// dest 支持三种形态：
//   - *[]T：T 为结构体或 map[string]any，一次填充整个切片；
//   - *T：T 为结构体，取第一行（结果集为空返回 ErrNoRows）；
//   - *[]any：填充为每行 map[string]any。
//
// 结构体字段匹配顺序：tushare tag → json tag → 字段名，匹配时忽略大小写与下划线。
// 支持的类型：string、数值、bool、time.Time、指针及 any；缺失值对值类型保持零值。
//
//	var bars []DailyRow
//	if err := tbl.Scan(&bars); err != nil { ... }
func (t *Table) Scan(dest any) error {
	if dest == nil {
		return fmt.Errorf("tushare: Scan 需要非空指针，实际为 nil")
	}
	rv := reflect.ValueOf(dest)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("tushare: Scan 需要非空指针，实际为 %s", rv.Kind())
	}
	target := rv.Elem()
	cols := t.columnIndex()

	switch target.Kind() {
	case reflect.Slice:
		elemType := target.Type().Elem()
		out := reflect.MakeSlice(target.Type(), 0, t.Len())
		if elemType.Kind() == reflect.Interface {
			// *[]any：每行填充为 map[string]any
			for i := range t.Items {
				out = reflect.Append(out, reflect.ValueOf(t.rowMap(i)))
			}
			target.Set(out)
			return nil
		}
		for i := range t.Items {
			v := reflect.New(elemType).Elem()
			if err := t.scanRow(v, i, cols); err != nil {
				return err
			}
			out = reflect.Append(out, v)
		}
		target.Set(out)
		return nil
	case reflect.Struct, reflect.Map:
		if t.Len() == 0 {
			return ErrNoRows
		}
		return t.scanRow(target, 0, cols)
	default:
		return fmt.Errorf("tushare: Scan 不支持的目标类型 %s（应为结构体或切片）", target.Kind())
	}
}

// ScanOne 是 Scan 的语义化别名：只取第一行。
func (t *Table) ScanOne(dest any) error {
	if t.Len() == 0 {
		return ErrNoRows
	}
	rv := reflect.ValueOf(dest)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("tushare: ScanOne 需要非空指针")
	}
	return t.scanRow(rv.Elem(), 0, t.columnIndex())
}

func (t *Table) columnIndex() map[string]int {
	m := make(map[string]int, len(t.Fields))
	for i, f := range t.Fields {
		key := normalizeKey(f)
		if _, ok := m[key]; !ok {
			m[key] = i
		}
	}
	return m
}

func (t *Table) scanRow(v reflect.Value, row int, cols map[string]int) error {
	values := t.Items[row]
	switch v.Kind() {
	case reflect.Map:
		kt := v.Type().Key()
		if kt.Kind() != reflect.String {
			return fmt.Errorf("tushare: Scan 目标 map 的 key 必须是 string")
		}
		if v.IsNil() {
			v.Set(reflect.MakeMap(v.Type()))
		}
		et := v.Type().Elem()
		for i, f := range t.Fields {
			if i >= len(values) {
				continue
			}
			val := reflect.ValueOf(values[i])
			if !val.IsValid() {
				v.SetMapIndex(reflect.ValueOf(f).Convert(kt), reflect.Zero(et))
				continue
			}
			if val.Type().AssignableTo(et) {
				v.SetMapIndex(reflect.ValueOf(f).Convert(kt), val)
			} else if et.Kind() == reflect.Interface && val.Type().Implements(et) {
				v.SetMapIndex(reflect.ValueOf(f).Convert(kt), val)
			} else if et.Kind() != reflect.Interface && val.Type().ConvertibleTo(et) {
				v.SetMapIndex(reflect.ValueOf(f).Convert(kt), val.Convert(et))
			}
		}
		return nil
	case reflect.Struct:
		vt := v.Type()
		for i := 0; i < vt.NumField(); i++ {
			sf := vt.Field(i)
			if sf.PkgPath != "" {
				continue
			}
			key := fieldKey(sf)
			if key == "" {
				continue
			}
			ci, ok := cols[key]
			if !ok || ci >= len(values) {
				continue
			}
			if err := assign(v.Field(i), values[ci]); err != nil {
				return fmt.Errorf("tushare: 字段 %s 赋值失败: %w", sf.Name, err)
			}
		}
		return nil
	default:
		return fmt.Errorf("tushare: Scan 不支持的目标类型 %s", v.Kind())
	}
}

// fieldKey 计算结构体字段与列名的匹配键。
func fieldKey(sf reflect.StructField) string {
	if tag, ok := sf.Tag.Lookup("tushare"); ok {
		if tag == "-" {
			return ""
		}
		if name := strings.Split(tag, ",")[0]; name != "" {
			return normalizeKey(name)
		}
	}
	if tag, ok := sf.Tag.Lookup("json"); ok {
		if tag == "-" {
			return ""
		}
		if name := strings.Split(tag, ",")[0]; name != "" {
			return normalizeKey(name)
		}
	}
	return normalizeKey(sf.Name)
}

// normalizeKey 统一列名/字段名：转小写并去掉下划线与连字符。
func normalizeKey(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '_', '-', ' ':
			continue
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

var timeType = reflect.TypeOf(time.Time{})

func assign(dst reflect.Value, src any) error {
	if !dst.CanSet() {
		return nil
	}
	if src == nil {
		switch dst.Kind() {
		case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice:
			dst.Set(reflect.Zero(dst.Type()))
		}
		return nil
	}
	if dst.Kind() == reflect.Pointer {
		p := reflect.New(dst.Type().Elem())
		if err := assign(p.Elem(), src); err != nil {
			return err
		}
		dst.Set(p)
		return nil
	}
	if dst.Type() == timeType {
		t, err := parseTime(toString(src))
		if err != nil {
			return err
		}
		dst.Set(reflect.ValueOf(t))
		return nil
	}
	switch dst.Kind() {
	case reflect.String:
		dst.SetString(toString(src))
	case reflect.Float32, reflect.Float64:
		f, err := toFloat(src)
		if err != nil {
			return err
		}
		dst.SetFloat(f)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := toInt(src)
		if err != nil {
			return err
		}
		dst.SetInt(i)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		i, err := toInt(src)
		if err != nil {
			return err
		}
		dst.SetUint(uint64(i))
	case reflect.Bool:
		b, err := toBool(src)
		if err != nil {
			return err
		}
		dst.SetBool(b)
	case reflect.Interface:
		dst.Set(reflect.ValueOf(src))
	default:
		return fmt.Errorf("不支持的目标类型 %s", dst.Kind())
	}
	return nil
}

func toString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case time.Time:
		return x.Format("20060102")
	default:
		return fmt.Sprint(x)
	}
}

func toFloat(v any) (float64, error) {
	switch x := v.(type) {
	case nil:
		return 0, nil
	case float64:
		return x, nil
	case int64:
		return float64(x), nil
	case int:
		return float64(x), nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return 0, nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("无法把 %q 解析为数值", x)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("无法把 %T 转换为数值", v)
	}
}

func toInt(v any) (int64, error) {
	switch x := v.(type) {
	case nil:
		return 0, nil
	case int64:
		return x, nil
	case int:
		return int64(x), nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return 0, fmt.Errorf("无法把 %v 转换为整数", x)
		}
		return int64(x), nil
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return 0, nil
		}
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return i, nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("无法把 %q 解析为整数", x)
		}
		return int64(f), nil
	default:
		return 0, fmt.Errorf("无法把 %T 转换为整数", v)
	}
}

func toBool(v any) (bool, error) {
	switch x := v.(type) {
	case nil:
		return false, nil
	case bool:
		return x, nil
	case int64:
		return x != 0, nil
	case float64:
		return x != 0, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "1", "true", "t", "y", "yes":
			return true, nil
		case "0", "false", "f", "n", "no", "":
			return false, nil
		default:
			return false, fmt.Errorf("无法把 %q 解析为布尔值", x)
		}
	default:
		return false, fmt.Errorf("无法把 %T 转换为布尔值", v)
	}
}

var timeLayouts = []string{
	"20060102",
	"2006-01-02",
	"20060102150405",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"20060102 15:04:05",
}

func parseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	for _, layout := range timeLayouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("无法把 %q 解析为时间", s)
}

// ToCSV 以 CSV 形式写出结果集（含表头），缺失值输出为空字符串。
func (t *Table) ToCSV(w io.Writer) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(t.Fields); err != nil {
		return err
	}
	rec := make([]string, len(t.Fields))
	for _, row := range t.Items {
		for i := range rec {
			if i < len(row) {
				rec[i] = toString(row[i])
			} else {
				rec[i] = ""
			}
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// String 返回前 10 行的等宽文本视图，便于日志与调试输出。
func (t *Table) String() string {
	const maxRows = 10
	if t == nil {
		return "<nil>"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s（%d 行 × %d 列）\n", t.API, t.Len(), len(t.Fields))
	if len(t.Fields) == 0 {
		return b.String()
	}
	tw := tabwriter.NewWriter(&b, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(t.Fields, "\t"))
	n := t.Len()
	if n > maxRows {
		n = maxRows
	}
	for i := 0; i < n; i++ {
		row := t.Items[i]
		cells := make([]string, len(t.Fields))
		for c := range cells {
			if c < len(row) {
				cells[c] = toString(row[c])
			}
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	_ = tw.Flush()
	if t.Len() > maxRows {
		fmt.Fprintf(&b, "... 省略 %d 行\n", t.Len()-maxRows)
	}
	if t.HasMore {
		b.WriteString("（服务端还有更多数据，可用 WithOffset/WithLimit 分页获取）\n")
	}
	return b.String()
}

// Row 是结果集中的一行，提供按列名取值的方法。
type Row struct {
	fields []string
	values []any
}

// Fields 返回列名。
func (r Row) Fields() []string { return r.fields }

// Values 返回原始值。
func (r Row) Values() []any { return r.values }

// Get 返回列 name 的原始值。
func (r Row) Get(name string) any {
	v, _ := r.lookup(name)
	return v
}

// Has 判断列是否存在且值非 nil。
func (r Row) Has(name string) bool {
	v, ok := r.lookup(name)
	return ok && v != nil
}

// Str 返回字符串值，缺失为 ""。
func (r Row) Str(name string) string {
	v, _ := r.lookup(name)
	return toString(v)
}

// Float 返回数值，缺失或无法解析为 0。
func (r Row) Float(name string) float64 {
	v, _ := r.lookup(name)
	f, err := toFloat(v)
	if err != nil {
		return 0
	}
	return f
}

// Int 返回整数值。
func (r Row) Int(name string) int64 {
	v, _ := r.lookup(name)
	i, err := toInt(v)
	if err != nil {
		return 0
	}
	return i
}

// Bool 返回布尔值。
func (r Row) Bool(name string) bool {
	v, _ := r.lookup(name)
	b, err := toBool(v)
	if err != nil {
		return false
	}
	return b
}

// Map 返回行数据的 map 形式。
func (r Row) Map() map[string]any {
	m := make(map[string]any, len(r.fields))
	for i, f := range r.fields {
		if i < len(r.values) {
			m[f] = r.values[i]
		}
	}
	return m
}

func (r Row) lookup(name string) (any, bool) {
	for i, f := range r.fields {
		if f == name {
			if i < len(r.values) {
				return r.values[i], true
			}
			return nil, false
		}
	}
	key := normalizeKey(name)
	for i, f := range r.fields {
		if normalizeKey(f) == key {
			if i < len(r.values) {
				return r.values[i], true
			}
			return nil, false
		}
	}
	return nil, false
}
