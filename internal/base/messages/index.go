package messages

import (
	"errors"
	"fmt"
	"reflect"
)

var errNilIndex = errors.New("索引 nil 值")

// strictIndex 与模板内建 index 一样逐层索引，但拒绝不存在的 map 键。
func strictIndex(item reflect.Value, indexes ...reflect.Value) (reflect.Value, error) {
	if !item.IsValid() {
		return reflect.Value{}, errNilIndex
	}
	for _, key := range indexes {
		for item.IsValid() && (item.Kind() == reflect.Interface || item.Kind() == reflect.Pointer) {
			if item.IsNil() {
				return reflect.Value{}, errNilIndex
			}
			item = item.Elem()
		}
		for key.IsValid() && key.Kind() == reflect.Interface {
			if key.IsNil() {
				key = reflect.Value{}
				break
			}
			key = key.Elem()
		}
		if !item.IsValid() {
			return reflect.Value{}, errNilIndex
		}
		switch item.Kind() {
		case reflect.Map:
			keyType := item.Type().Key()
			if !key.IsValid() {
				switch keyType.Kind() {
				case reflect.Interface, reflect.Pointer, reflect.Chan:
					key = reflect.Zero(keyType)
				default:
					return reflect.Value{}, fmt.Errorf("nil 不是有效的 %s 键", keyType)
				}
			}
			if !key.Type().AssignableTo(keyType) {
				if integerKind(key.Kind()) && integerKind(keyType.Kind()) && key.Type().ConvertibleTo(keyType) {
					key = key.Convert(keyType)
				} else {
					return reflect.Value{}, fmt.Errorf("索引类型 %s 与 %s 不匹配", key.Type(), keyType)
				}
			}
			if !key.Comparable() {
				return reflect.Value{}, fmt.Errorf("索引类型 %s 不可比较", key.Type())
			}
			value := item.MapIndex(key)
			if !value.IsValid() {
				return reflect.Value{}, fmt.Errorf("map 不含键 %v", key.Interface())
			}
			item = value
		case reflect.Array, reflect.Slice, reflect.String:
			var index uint64
			switch key.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				if key.Int() < 0 {
					return reflect.Value{}, fmt.Errorf("索引为负数")
				}
				//nolint:gosec // G115：前面已检查负数。
				index = uint64(key.Int())
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
				index = key.Uint()
			default:
				return reflect.Value{}, fmt.Errorf("序列索引不是整数")
			}
			//nolint:gosec // G115：Len 返回非负的 int。
			if index >= uint64(item.Len()) {
				return reflect.Value{}, fmt.Errorf("序列索引越界")
			}
			//nolint:gosec // G115：索引已限制在 int 类型的 Len 以内。
			item = item.Index(int(index))
		default:
			return reflect.Value{}, fmt.Errorf("无法索引 %s", item.Type())
		}
	}
	return item, nil
}

func integerKind(kind reflect.Kind) bool {
	return kind >= reflect.Int && kind <= reflect.Int64 || kind >= reflect.Uint && kind <= reflect.Uintptr
}
