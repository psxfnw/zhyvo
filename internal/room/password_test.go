package room

import "testing"

func TestHashAndVerifySecret(t *testing.T) {
	encoded, err := hashSecret("4826")
	if err != nil {
		t.Fatalf("hashSecret() error = %v", err)
	}

	matches, err := verifySecret(encoded, "4826")
	if err != nil {
		t.Fatalf("verifySecret() error = %v", err)
	}
	if !matches {
		t.Fatal("verifySecret() = false, want true")
	}

	matches, err = verifySecret(encoded, "1111")
	if err != nil {
		t.Fatalf("verifySecret() wrong password error = %v", err)
	}
	if matches {
		t.Fatal("verifySecret() wrong password = true, want false")
	}
}

func TestValidateCreate(t *testing.T) {
	tests := []struct {
		name    string
		input   CreateInput
		wantErr bool
	}{
		{name: "public", input: CreateInput{Name: "Event", LifetimeDays: 1, AccessMode: "public"}},
		{name: "pin", input: CreateInput{Name: "Event", LifetimeDays: 2, AccessMode: "pin", Secret: "4826"}},
		{name: "password", input: CreateInput{Name: "Event", LifetimeDays: 3, AccessMode: "password", Secret: "strong-password"}},
		{name: "invalid lifetime", input: CreateInput{Name: "Event", LifetimeDays: 4, AccessMode: "public"}, wantErr: true},
		{name: "short pin", input: CreateInput{Name: "Event", LifetimeDays: 1, AccessMode: "pin", Secret: "123"}, wantErr: true},
		{name: "public with secret", input: CreateInput{Name: "Event", LifetimeDays: 1, AccessMode: "public", Secret: "unexpected"}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateCreate(test.input)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateCreate() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}
