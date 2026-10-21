package main

import "testing"

func TestValidateInput(t *testing.T) {
	tests := []struct {
		name    string
		input   ItemInput
		wantErr bool
	}{
		{name: "valid", input: ItemInput{Name: "demo", Description: "test"}},
		{name: "blank name", input: ItemInput{Name: "   "}, wantErr: true},
		{name: "name too long", input: ItemInput{Name: string(make([]byte, 201))}, wantErr: true},
		{name: "description too long", input: ItemInput{Name: "demo", Description: string(make([]byte, 10001))}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateInput(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateInput() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
