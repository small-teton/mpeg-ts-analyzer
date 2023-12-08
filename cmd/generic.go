package cmd

import (
	"fmt"

	"golang.org/x/exp/constraints"
)

func genericTest() {

	// var hoge uint16 = 1
	// valueType(hoge)
	// typeParameterType[uint32](1)
}

/////////////////////////////////////////////////
// 今までのGoの書き方

func containsInt(needle int, array []int) bool {
	for _, v := range array {
		if v == needle {
			return true
		}
	}
	return false
}

func containsInt64(needle int64, array []int64) bool {
	for _, v := range array {
		if v == needle {
			return true
		}
	}
	return false
}

/////////////////////////////////////////////////
// go.1.18で導入されたGenericを使うと・・・

func genericContains[T comparable](needle T, array []T) bool {
	for _, v := range array {
		if v == needle {
			return true
		}
	}
	return false
}

/////////////////////////////////////////////////
// 午前中に調査していたこと
// 関数内での型判定は出来るのか？

// 引数の値から調べる
// goはコンパイル時に呼び出しに応じてTの型が決定される
func valueType[T constraints.Unsigned](input T) T {
	switch any(input).(type) {
	case uint8:
		fmt.Println("type: uint8")
	case uint16:
		fmt.Println("type: uint16")
	case uint32:
		fmt.Println("type: uint32")
	case uint64:
		fmt.Println("type: uint64")
	default:
		fmt.Println("type: other")
	}
	return 1
}

// 値ではなく型引数の指定情報を取得できるのか？
func typeParameterType[T constraints.Unsigned](input uint) T {
	var t interface{} = *new(T)
	switch t.(type) {
	case uint8:
		fmt.Println("type parameter: uint8")
		var retVal uint8
		return T(retVal)
	case uint16:
		fmt.Println("type parameter: uint16")
		var retVal uint16
		return T(retVal)
	case uint32:
		fmt.Println("type parameter: uint32")
		var retVal uint32
		return T(retVal)
	case uint64:
		fmt.Println("type parameter: uint64")
		var retVal uint64
		return T(retVal)
	default:
		fmt.Println("type parameter: other")
	}
	return 1
}
