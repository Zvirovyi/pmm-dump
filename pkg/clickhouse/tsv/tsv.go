package tsv

import (
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type Reader struct {
	*csv.Reader
}

type Writer struct {
	*csv.Writer
}

func NewWriter(w io.Writer) *Writer {
	writer := csv.NewWriter(w)
	writer.Comma = '\t'
	return &Writer{writer}
}

func NewReader(r io.Reader) *Reader {
	reader := csv.NewReader(r)
	reader.Comma = '\t'
	reader.FieldsPerRecord = 0
	return &Reader{reader}
}

func (r *Reader) Read(ct []*sql.ColumnType) ([]interface{}, error) {
	records, err := r.Reader.Read()
	if err != nil {
		return nil, err
	}
	if len(ct) != len(records) {
		return nil, errors.New("amount of columns mismatch")
	}

	values := make([]interface{}, 0, len(records))
	for i, record := range records {
		st := ct[i].ScanType()
		value, err := parseElement(record, st)
		if err != nil {
			return nil, errors.New(fmt.Sprintf("parsing error: %s", err.Error()))
		}
		values = append(values, value)
	}

	return values, nil
}

func parseSlice(slice string, st reflect.Type) (interface{}, error) {
	slice = strings.TrimSpace(slice[1 : len(slice)-1])
	elements := strings.Split(slice, ",")
	result := make([]interface{}, 0, len(elements))
	if slice == "" {
		return result, nil
	}
	for _, v := range elements {
		value, err := parseElement(v, st)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func parseElement(record string, st reflect.Type) (value interface{}, err error) {
	switch st.Kind() {
	case reflect.Slice:
		value, err = parseSlice(record, st.Elem())
		if err != nil {
			return nil, err
		}
	case reflect.Int8:
		result, err := strconv.ParseInt(record, 10, 8)
		if err != nil {
			return nil, err
		}
		value = int8(result)
	case reflect.Int16:
		result, err := strconv.ParseInt(record, 10, 16)
		if err != nil {
			return nil, err
		}
		value = int16(result)
	case reflect.Int32:
		result, err := strconv.ParseInt(record, 10, 32)
		if err != nil {
			return nil, err
		}
		value = int32(result)
	case reflect.Int64:
		value, err = strconv.ParseInt(record, 10, 64)
		if err != nil {
			return nil, err
		}
	case reflect.Uint8:
		result, err := strconv.ParseUint(record, 10, 8)
		if err != nil {
			return nil, err
		}
		value = uint8(result)
	case reflect.Uint16:
		result, err := strconv.ParseUint(record, 10, 16)
		if err != nil {
			return nil, err
		}
		value = uint16(result)
	case reflect.Uint32:
		result, err := strconv.ParseUint(record, 10, 32)
		if err != nil {
			return nil, err
		}
		value = uint32(result)
	case reflect.Uint64:
		value, err = strconv.ParseUint(record, 10, 64)
		if err != nil {
			return nil, err
		}
	case reflect.Float32:
		result, err := strconv.ParseFloat(record, 32)
		if err != nil {
			return nil, err
		}
		value = float32(result)
	case reflect.Float64:
		value, err = strconv.ParseFloat(record, 64)
		if err != nil {
			return nil, err
		}
	case reflect.String:
		value = record
	default:
		switch st.Name() {
		case "Time":
			value, err = time.Parse("2006-01-02 15:04:05 -0700 UTC", record)
			if err != nil {
				return nil, err
			}
		default:
			return nil, errors.New("unknown type")
		}
	}
	return
}
