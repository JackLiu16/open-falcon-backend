package rpc

import (
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Tests handling of panic to error", func() {
	DescribeTable("Error Message",
		func(errorObj interface{}, expectedContent string) {
			testedError := samplePanic(errorObj)

			Expect(testedError).To(HaveOccurred())
			Expect(testedError.Error()).To(ContainSubstring(expectedContent))
		},
		Entry("String value as error", "P1-err-C1", "P1-err-C1"),
		Entry("Error object", errors.New("E1-err-G5"), "E1-err-G5"),
	)
})

func samplePanic(samplePanic interface{}) (err error) {
	defer HandleError(&err)()
	panic(samplePanic)
}

func ExampleHanleError() {
	sampleFunc := func() (err error) {
		defer HandleError(&err)()

		panic("This is panic")
	}

	err := sampleFunc()
	fmt.Printf("%v\n", err)

	// Output:
	// Has error on RPC: This is panic
}
