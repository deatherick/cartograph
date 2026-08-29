package ts

import (
	"context"
	"testing"
)

const sample = `
import { UserRepository } from "../repositories/userRepository";
import { EmailService, welcomeEmail } from "./emailService";
import { CreateUserInput, User, isAdmin } from "../models/user";
import { assertValidEmail, isNonEmpty, ValidationError } from "../utils/validation";

function hashPassword(password: string): string {
  return "hashed:" + password;
}

export class UserService {
  constructor(private repo: UserRepository, private emailService: EmailService) {}

  register(input: CreateUserInput): User {
    assertValidEmail(input.email);
    if (!isNonEmpty(input.name)) {
      throw new ValidationError("name", "name is required");
    }
    if (this.repo.findByEmail(input.email)) {
      throw new ValidationError("email", "email already registered");
    }
    const user = this.repo.insert(input, hashPassword(input.password));
    this.emailService.send(welcomeEmail(user.name, user.email));
    return user;
  }
}

export interface Extra extends Base {
  x: number;
}
`

func TestExtract_EntitiesRefsImports(t *testing.T) {
	e := New()
	facts, err := e.Extract(context.Background(), "test-repo", "src/services/userService.ts", []byte(sample))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	wantKinds := map[string]string{
		"hashPassword":                  "Function",
		"UserService":                   "Class",
		"src/services/userService.ts#UserService.register": "Method",
		"Extra":                         "Interface",
	}
	got := map[string]string{}
	for _, e := range facts.Entities {
		got[e.Name] = string(e.Kind)
		if e.Qualified == "src/services/userService.ts#UserService.register" {
			got[e.Qualified] = string(e.Kind)
		}
	}
	for name, kind := range wantKinds {
		if got[name] != kind {
			t.Errorf("entity %q: got kind %q, want %q (all entities: %+v)", name, got[name], kind, facts.Entities)
		}
	}

	if len(facts.Imports) == 0 {
		t.Fatal("expected at least one import binding, got none")
	}
	foundNamedImport := false
	for _, im := range facts.Imports {
		if im.LocalName == "UserRepository" && im.Source == "../repositories/userRepository" {
			foundNamedImport = true
		}
	}
	if !foundNamedImport {
		t.Errorf("expected an import binding for UserRepository from ../repositories/userRepository, got %+v", facts.Imports)
	}

	foundExtends := false
	for _, r := range facts.Refs {
		if string(r.Kind) == "extends" && r.Target.Name == "Base" {
			foundExtends = true
		}
	}
	if !foundExtends {
		t.Errorf("expected an extends ref to Base, got refs: %+v", facts.Refs)
	}

	foundQualifiedCall := false
	for _, r := range facts.Refs {
		if string(r.Kind) == "call" && r.Target.Name == "repo" && r.Target.Member == "findByEmail" {
			foundQualifiedCall = true
		}
	}
	if !foundQualifiedCall {
		t.Errorf("expected a qualified call ref repo.findByEmail, got refs: %+v", facts.Refs)
	}

	foundBareCall := false
	for _, r := range facts.Refs {
		if string(r.Kind) == "call" && r.Target.Name == "hashPassword" {
			foundBareCall = true
		}
	}
	if !foundBareCall {
		t.Errorf("expected a bare call ref to hashPassword, got refs: %+v", facts.Refs)
	}
}

func TestExtract_MalformedInput_HighErrorRatio(t *testing.T) {
	e := New()
	// Deliberately garbage input to exercise the parser's error-ratio gate
	// (internal/parser's package doc) rather than silently producing junk
	// entities.
	garbage := "{{{ ][ )( class class class ((( }}} +++ ---"
	_, err := e.Extract(context.Background(), "test-repo", "garbage.ts", []byte(garbage))
	if err == nil {
		t.Fatal("expected an error for high-syntax-error-ratio input, got nil")
	}
}

func TestExtract_EmptyFile(t *testing.T) {
	e := New()
	facts, err := e.Extract(context.Background(), "test-repo", "empty.ts", []byte(""))
	if err != nil {
		t.Fatalf("Extract on empty file should not error: %v", err)
	}
	if len(facts.Entities) != 0 {
		t.Errorf("expected no entities in an empty file, got %+v", facts.Entities)
	}
}

const mongooseSample = `
const UserSchema = new Schema({});

UserSchema.methods.validPassword = function (password: string): boolean {
  const hash = crypto.pbkdf2Sync(password, this.salt, 10000, 512, 'sha512').toString('hex');
  return this.hash === hash;
};

UserSchema.methods.generateJWT = function (): string {
  return jwt.sign({id: this._id}, JWT_SECRET);
};
`

func TestExtract_MongooseSchemaMethodAssignment(t *testing.T) {
	e := New()
	facts, err := e.Extract(context.Background(), "test-repo", "src/models/user.model.ts", []byte(mongooseSample))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	var found []string
	for _, ent := range facts.Entities {
		if ent.Kind == "Method" {
			found = append(found, ent.Qualified)
		}
	}
	want := []string{
		"src/models/user.model.ts#UserSchema.validPassword",
		"src/models/user.model.ts#UserSchema.generateJWT",
	}
	for _, w := range want {
		ok := false
		for _, f := range found {
			if f == w {
				ok = true
			}
		}
		if !ok {
			t.Errorf("expected method entity %q, found: %+v", w, found)
		}
	}

	// A call made INSIDE one of these methods (jwt.sign) must be attributed
	// to that method's Src, not left at module scope — this is the
	// enclosingScope fix for function_expression scope nodes.
	foundAttributedCall := false
	for _, r := range facts.Refs {
		if r.Target.Name == "jwt" && r.Target.Member == "sign" && r.Src != "" {
			foundAttributedCall = true
		}
	}
	if !foundAttributedCall {
		t.Errorf("expected the jwt.sign call inside generateJWT to have a non-empty Src, refs: %+v", facts.Refs)
	}
}
