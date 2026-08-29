package ts

import (
	"context"
	"testing"

	"github.com/deatherick/cartograph/internal/model"
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

func TestExtract_ReceiverType_ConstructorProperty(t *testing.T) {
	src := `
export class UserService {
  constructor(private repo: UserRepository, private emailService: EmailService) {}

  register(input: string): void {
    this.repo.findByEmail(input);
  }
}
`
	e := New()
	facts, err := e.Extract(context.Background(), "repo", "svc.ts", []byte(src))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	found := false
	for _, r := range facts.Refs {
		if r.Target.Name == "repo" && r.Target.Member == "findByEmail" {
			found = true
			if r.Target.ReceiverType != "UserRepository" {
				t.Errorf("expected ReceiverType=UserRepository, got %q", r.Target.ReceiverType)
			}
		}
	}
	if !found {
		t.Fatalf("expected a call ref to repo.findByEmail, got: %+v", facts.Refs)
	}
}

func TestExtract_ReceiverType_TypedField(t *testing.T) {
	src := `
export class OrderService {
  private orders: OrderRepository;

  place(): void {
    this.orders.insert(null);
  }
}
`
	e := New()
	facts, err := e.Extract(context.Background(), "repo", "svc.ts", []byte(src))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	found := false
	for _, r := range facts.Refs {
		if r.Target.Name == "orders" && r.Target.Member == "insert" {
			found = true
			if r.Target.ReceiverType != "OrderRepository" {
				t.Errorf("expected ReceiverType=OrderRepository, got %q", r.Target.ReceiverType)
			}
		}
	}
	if !found {
		t.Fatalf("expected a call ref to orders.insert, got: %+v", facts.Refs)
	}
}

func TestExtract_ReceiverType_TypedVariable(t *testing.T) {
	src := `
function run(): void {
  const svc: UserService = getService();
  svc.register("x");
}
`
	e := New()
	facts, err := e.Extract(context.Background(), "repo", "svc.ts", []byte(src))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	found := false
	for _, r := range facts.Refs {
		if r.Target.Name == "svc" && r.Target.Member == "register" {
			found = true
			if r.Target.ReceiverType != "UserService" {
				t.Errorf("expected ReceiverType=UserService, got %q", r.Target.ReceiverType)
			}
		}
	}
	if !found {
		t.Fatalf("expected a call ref to svc.register, got: %+v", facts.Refs)
	}
}

func TestExtract_ReceiverType_NewInitializedVariable(t *testing.T) {
	src := `
function run(): void {
  const svc = new UserService();
  svc.register("x");
}
`
	e := New()
	facts, err := e.Extract(context.Background(), "repo", "svc.ts", []byte(src))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	found := false
	for _, r := range facts.Refs {
		if r.Target.Name == "svc" && r.Target.Member == "register" {
			found = true
			if r.Target.ReceiverType != "UserService" {
				t.Errorf("expected ReceiverType=UserService, got %q", r.Target.ReceiverType)
			}
		}
	}
	if !found {
		t.Fatalf("expected a call ref to svc.register, got: %+v", facts.Refs)
	}
}

func TestExtract_ReceiverType_AmbiguousVariableName_NotInferred(t *testing.T) {
	// `x` is typed differently in two different functions in the same
	// file — fileVarTypes must NOT collapse this to either type; the
	// resolver must not guess (docs/research/03's whitelist-not-guess
	// principle).
	src := `
function a(): void {
  const x: Foo = getFoo();
  x.method();
}
function b(): void {
  const x: Bar = getBar();
  x.method();
}
`
	e := New()
	facts, err := e.Extract(context.Background(), "repo", "svc.ts", []byte(src))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	for _, r := range facts.Refs {
		if r.Target.Name == "x" && r.Target.Member == "method" && r.Target.ReceiverType != "" {
			t.Errorf("expected ReceiverType to stay empty for an ambiguously-typed variable name, got %q", r.Target.ReceiverType)
		}
	}
}

func TestExtract_CJSRequire_Default(t *testing.T) {
	src := `const UserRepository = require('./userRepository');`
	e := New()
	facts, err := e.Extract(context.Background(), "repo", "a.ts", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, im := range facts.Imports {
		if im.LocalName == "UserRepository" && im.Source == "./userRepository" && im.IsDefault {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a CJS default require binding, got %+v", facts.Imports)
	}
}

func TestExtract_CJSRequire_Destructured(t *testing.T) {
	src := `const { authentication, other } = require('./auth');`
	e := New()
	facts, err := e.Extract(context.Background(), "repo", "a.ts", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, im := range facts.Imports {
		if im.Source == "./auth" {
			names[im.LocalName] = true
		}
	}
	if !names["authentication"] || !names["other"] {
		t.Fatalf("expected both destructured require bindings, got %+v", facts.Imports)
	}
}

func TestExtract_ReExport_Star(t *testing.T) {
	src := `export * from './userModel';`
	e := New()
	facts, err := e.Extract(context.Background(), "repo", "a.ts", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(facts.ReExports) != 1 || !facts.ReExports[0].IsStar || facts.ReExports[0].Source != "./userModel" {
		t.Fatalf("expected one star re-export from ./userModel, got %+v", facts.ReExports)
	}
}

func TestExtract_ReExport_Named(t *testing.T) {
	src := `export { User, Order as OrderModel } from './models';`
	e := New()
	facts, err := e.Extract(context.Background(), "repo", "a.ts", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(facts.ReExports) != 2 {
		t.Fatalf("expected 2 named re-exports, got %+v", facts.ReExports)
	}
	byName := map[string]model.ReExport{}
	for _, r := range facts.ReExports {
		byName[r.ExportedName] = r
	}
	if byName["User"].LocalAlias != "User" || byName["User"].IsStar {
		t.Errorf("expected User re-exported as-is, got %+v", byName["User"])
	}
	if byName["Order"].LocalAlias != "OrderModel" {
		t.Errorf("expected Order re-exported as OrderModel, got %+v", byName["Order"])
	}
}

func TestExtract_TestDetection(t *testing.T) {
	src := `
describe("UserService", () => {
  it("registers a user", () => {
    expect(true).toBe(true);
  });
  test("rejects invalid email", () => {
    expect(false).toBe(false);
  });
});
`
	e := New()
	facts, err := e.Extract(context.Background(), "repo", "a.test.ts", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, ent := range facts.Entities {
		if ent.Kind == model.KindTest {
			names[ent.Name] = true
		}
	}
	for _, want := range []string{"UserService", "registers a user", "rejects invalid email"} {
		if !names[want] {
			t.Errorf("expected a Test entity named %q, found: %v", want, names)
		}
	}
}

// TestExtract_SchemaStyleConst mirrors the exact real-repo pattern
// (typescript-node-express-realworld-example-app's user.model.ts) that
// motivated edge-case-backlog.md I11: a Mongoose model exported as a plain
// `const`, invisible to every other file that imports it before this fix.
func TestExtract_SchemaStyleConst(t *testing.T) {
	src := `
import { model, Model, Schema } from "mongoose";

const UserSchema = new Schema({ username: String });

export const User: Model<IUserModel> = model<IUserModel>("User", UserSchema);

export const Order = mongoose.model("Order", OrderSchema);

function helper() {
  const local = someFactory("not a top-level entity");
  return local;
}
`
	e := New()
	facts, err := e.Extract(context.Background(), "repo", "models/user.model.ts", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]model.Kind{}
	for _, ent := range facts.Entities {
		kinds[ent.Name] = ent.Kind
	}
	if kinds["User"] != model.KindClass {
		t.Errorf("expected User (bare factory call, generic type args) to be extracted as KindClass, got %+v", kinds)
	}
	if kinds["Order"] != model.KindClass {
		t.Errorf("expected Order (qualified mongoose.model call) to be extracted as KindClass, got %+v", kinds)
	}
	if _, ok := kinds["local"]; ok {
		t.Errorf("expected 'local' (inside a function body) to NOT be extracted as a top-level entity, got %+v", kinds)
	}
}
