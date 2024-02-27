package cedar

import (
	"bytes"
	"encoding/json"
	"fmt"
)

var (
	errJSONInvalidExtn     = fmt.Errorf("invalid extension")
	errJSONDecode          = fmt.Errorf("error decoding json")
	errJSONLongOutOfRange  = fmt.Errorf("long out of range")
	errJSONUnsupportedType = fmt.Errorf("unsupported type")
	errJSONExtFnMatch      = fmt.Errorf("json extn mismatch")
	errJSONExtNotFound     = fmt.Errorf("json extn not found")
	errJSONEntityNotFound  = fmt.Errorf("json entity not found")
)

type extn struct {
	Fn  string `json:"fn"`
	Arg string `json:"arg"`
}

type extValueJSON struct {
	Extn *extn `json:"__extn,omitempty"`
}

type extEntity struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type entityValueJSON struct {
	Type   *string    `json:"type,omitempty"`
	ID     *string    `json:"id,omitempty"`
	Entity *extEntity `json:"__entity,omitempty"`
}

type explicitValue struct {
	Value Value
}

func unmarshalJSON(b []byte, v *Value) error {
	// TODO: make this faster if it matters
	{
		var res EntityUID
		ptr := &res
		if err := ptr.UnmarshalJSON(b); err == nil {
			*v = res
			return nil
		}
	}
	{
		var res extValueJSON
		if err := json.Unmarshal(b, &res); err == nil && res.Extn != nil {
			switch res.Extn.Fn {
			case "ip":
				val, err := newIPValue(res.Extn.Arg)
				if err != nil {
					return err
				}
				*v = val
				return nil
			case "decimal":
				val, err := newDecimalValue(res.Extn.Arg)
				if err != nil {
					return err
				}
				*v = val
				return nil
			default:
				return errJSONInvalidExtn
			}
		}
	}

	if len(b) > 0 {
		switch b[0] {
		case '[':
			var res Set
			err := json.Unmarshal(b, &res)
			*v = res
			return err
		case '{':
			res := Record{}
			err := json.Unmarshal(b, &res)
			*v = res
			return err
		}
	}

	var res interface{}
	dec := json.NewDecoder(bytes.NewBuffer(b))
	dec.UseNumber()
	if err := dec.Decode(&res); err != nil {
		return fmt.Errorf("%w: %w", errJSONDecode, err)
	}
	switch vv := res.(type) {
	case string:
		*v = String(vv)
	case bool:
		*v = Boolean(vv)
	case json.Number:
		l, err := vv.Int64()
		if err != nil {
			return fmt.Errorf("%w: %w", errJSONLongOutOfRange, err)
		}
		*v = Long(l)
	default:
		return errJSONUnsupportedType
	}
	return nil
}
