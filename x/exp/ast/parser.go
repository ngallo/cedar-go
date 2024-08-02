package ast

import (
	"fmt"
	"net/netip"
	"strconv"

	"github.com/cedar-policy/cedar-go/types"
)

func policyFromCedar(p *parser) (*Policy, error) {
	annotations, err := p.annotations()
	if err != nil {
		return nil, err
	}

	policy, err := p.effect(&annotations)
	if err != nil {
		return nil, err
	}

	if err = p.exact("("); err != nil {
		return nil, err
	}
	if err = p.principal(policy); err != nil {
		return nil, err
	}
	if err = p.exact(","); err != nil {
		return nil, err
	}
	if err = p.action(policy); err != nil {
		return nil, err
	}
	if err = p.exact(","); err != nil {
		return nil, err
	}
	if err = p.resource(policy); err != nil {
		return nil, err
	}
	if err = p.exact(")"); err != nil {
		return nil, err
	}
	if err = p.conditions(policy); err != nil {
		return nil, err
	}
	if err = p.exact(";"); err != nil {
		return nil, err
	}

	return policy, nil
}

type parser struct {
	tokens []Token
	pos    int
}

func newParser(tokens []Token) parser {
	return parser{tokens: tokens, pos: 0}
}

func (p *parser) advance() Token {
	t := p.peek()
	if p.pos < len(p.tokens)-1 {
		p.pos++
	}
	return t
}

func (p *parser) peek() Token {
	return p.tokens[p.pos]
}

func (p *parser) exact(tok string) error {
	t := p.advance()
	if t.Text != tok {
		return p.errorf("exact got %v want %v", t.Text, tok)
	}
	return nil
}

func (p *parser) errorf(s string, args ...interface{}) error {
	var t Token
	if p.pos < len(p.tokens) {
		t = p.tokens[p.pos]
	}
	err := fmt.Errorf(s, args...)
	return fmt.Errorf("parse error at %v %q: %w", t.Pos, t.Text, err)
}

func (p *parser) annotations() (Annotations, error) {
	var res Annotations
	for p.peek().Text == "@" {
		p.advance()
		err := p.annotation(&res)
		if err != nil {
			return res, err
		}
	}
	return res, nil

}

func (p *parser) annotation(a *Annotations) error {
	var err error
	t := p.advance()
	if !t.isIdent() {
		return p.errorf("expected ident")
	}
	name := types.String(t.Text)
	if err = p.exact("("); err != nil {
		return err
	}
	t = p.advance()
	if !t.isString() {
		return p.errorf("expected string")
	}
	value, err := t.stringValue()
	if err != nil {
		return err
	}
	if err = p.exact(")"); err != nil {
		return err
	}

	a.Annotation(name, types.String(value))
	return nil
}

func (p *parser) effect(a *Annotations) (*Policy, error) {
	next := p.advance()
	if next.Text == "permit" {
		return a.Permit(), nil
	} else if next.Text == "forbid" {
		return a.Forbid(), nil
	}

	return nil, p.errorf("unexpected effect: %v", next.Text)
}

func (p *parser) principal(policy *Policy) error {
	if err := p.exact("principal"); err != nil {
		return err
	}
	switch p.peek().Text {
	case "==":
		p.advance()
		entity, err := p.entity()
		if err != nil {
			return err
		}
		policy.PrincipalEq(entity)
		return nil
	case "is":
		p.advance()
		path, err := p.path()
		if err != nil {
			return err
		}
		if p.peek().Text == "in" {
			p.advance()
			entity, err := p.entity()
			if err != nil {
				return err
			}
			policy.PrincipalIsIn(path, entity)
			return nil
		}

		policy.PrincipalIs(path)
		return nil
	case "in":
		p.advance()
		entity, err := p.entity()
		if err != nil {
			return err
		}
		policy.PrincipalIn(entity)
		return nil
	}

	return nil
}

func (p *parser) entity() (types.EntityUID, error) {
	var res types.EntityUID
	t := p.advance()
	if !t.isIdent() {
		return res, p.errorf("expected ident")
	}
	return p.entityFirstPathPreread(t.Text)
}

func (p *parser) entityFirstPathPreread(firstPath string) (types.EntityUID, error) {
	var res types.EntityUID
	var err error
	res.Type = firstPath
	for {
		if err := p.exact("::"); err != nil {
			return res, err
		}
		t := p.advance()
		switch {
		case t.isIdent():
			res.Type = fmt.Sprintf("%v::%v", res.Type, t.Text)
		case t.isString():
			res.ID, err = t.stringValue()
			if err != nil {
				return res, err
			}
			return res, nil
		default:
			return res, p.errorf("unexpected token")
		}
	}
}

func (p *parser) path() (types.String, error) {
	var res types.String
	t := p.advance()
	if !t.isIdent() {
		return res, p.errorf("expected ident")
	}
	res = types.String(t.Text)
	for {
		if p.peek().Text != "::" {
			return res, nil
		}
		p.advance()
		t := p.advance()
		switch {
		case t.isIdent():
			res = types.String(fmt.Sprintf("%v::%v", res, t.Text))
		default:
			return res, p.errorf("unexpected token")
		}
	}
}

func (p *parser) action(policy *Policy) error {
	if err := p.exact("action"); err != nil {
		return err
	}
	switch p.peek().Text {
	case "==":
		p.advance()
		entity, err := p.entity()
		if err != nil {
			return err
		}
		policy.ActionEq(entity)
		return nil
	case "in":
		p.advance()
		if p.peek().Text == "[" {
			p.advance()
			entities, err := p.entlist()
			if err != nil {
				return err
			}
			policy.ActionInSet(entities...)
			p.advance() // entlist guarantees "]"
			return nil
		} else {
			entity, err := p.entity()
			if err != nil {
				return err
			}
			policy.ActionIn(entity)
			return nil
		}
	}

	return nil
}

func (p *parser) entlist() ([]types.EntityUID, error) {
	var res []types.EntityUID
	for p.peek().Text != "]" {
		if len(res) > 0 {
			if err := p.exact(","); err != nil {
				return nil, err
			}
		}
		e, err := p.entity()
		if err != nil {
			return nil, err
		}
		res = append(res, e)
	}
	return res, nil
}

func (p *parser) resource(policy *Policy) error {
	if err := p.exact("resource"); err != nil {
		return err
	}
	switch p.peek().Text {
	case "==":
		p.advance()
		entity, err := p.entity()
		if err != nil {
			return err
		}
		policy.ResourceEq(entity)
		return nil
	case "is":
		p.advance()
		path, err := p.path()
		if err != nil {
			return err
		}
		if p.peek().Text == "in" {
			p.advance()
			entity, err := p.entity()
			if err != nil {
				return err
			}
			policy.ResourceIsIn(path, entity)
			return nil
		}

		policy.ResourceIs(path)
		return nil
	case "in":
		p.advance()
		entity, err := p.entity()
		if err != nil {
			return err
		}
		policy.ResourceIn(entity)
		return nil
	}

	return nil
}

func (p *parser) conditions(policy *Policy) error {
	for {
		switch p.peek().Text {
		case "when":
			p.advance()
			expr, err := p.condition()
			if err != nil {
				return err
			}
			policy.When(expr)
		case "unless":
			p.advance()
			expr, err := p.condition()
			if err != nil {
				return err
			}
			policy.Unless(expr)
		default:
			return nil
		}
	}
}

func (p *parser) condition() (Node, error) {
	var res Node
	var err error
	if err := p.exact("{"); err != nil {
		return res, err
	}
	if res, err = p.expression(); err != nil {
		return res, err
	}
	if err := p.exact("}"); err != nil {
		return res, err
	}
	return res, nil
}

func (p *parser) expression() (Node, error) {
	t := p.peek()
	if t.Text == "if" {
		p.advance()

		condition, err := p.expression()
		if err != nil {
			return Node{}, err
		}

		if err = p.exact("then"); err != nil {
			return Node{}, err
		}
		ifTrue, err := p.expression()
		if err != nil {
			return Node{}, err
		}

		if err = p.exact("else"); err != nil {
			return Node{}, err
		}
		ifFalse, err := p.expression()
		if err != nil {
			return Node{}, err
		}

		return If(condition, ifTrue, ifFalse), nil
	}

	return p.or()
}

func (p *parser) or() (Node, error) {
	lhs, err := p.and()
	if err != nil {
		return Node{}, err
	}

	t := p.peek()
	if t.Text != "||" {
		return lhs, nil
	}

	p.advance()
	rhs, err := p.and()
	if err != nil {
		return Node{}, err
	}
	return lhs.Or(rhs), nil
}

func (p *parser) and() (Node, error) {
	lhs, err := p.relation()
	if err != nil {
		return Node{}, err
	}

	t := p.peek()
	if t.Text != "&&" {
		return lhs, nil
	}

	p.advance()
	rhs, err := p.relation()
	if err != nil {
		return Node{}, err
	}
	return lhs.And(rhs), nil
}

func (p *parser) relation() (Node, error) {
	lhs, err := p.add()
	if err != nil {
		return Node{}, err
	}

	t := p.peek()
	operators := map[string]func(Node) Node{
		"<":  lhs.LessThan,
		"<=": lhs.LessThanOrEqual,
		">":  lhs.GreaterThan,
		">=": lhs.GreaterThanOrEqual,
		"!=": lhs.NotEquals,
		"==": lhs.Equals,
		"in": lhs.In,
	}
	if f, ok := operators[t.Text]; ok {
		p.advance()
		rhs, err := p.add()
		if err != nil {
			return Node{}, err
		}
		return f(rhs), nil
	}

	if t.Text == "has" {
		p.advance()
		t = p.advance()
		if t.isIdent() {
			return lhs.Has(t.Text), nil
		} else if t.isString() {
			str, err := t.stringValue()
			if err != nil {
				return Node{}, err
			}
			return lhs.Has(str), nil
		}
		return Node{}, p.errorf("expected ident or string")
	} else if t.Text == "like" {
		// TODO: Deal with pattern matching
		return Node{}, p.errorf("unimplemented")
	} else if t.Text == "is" {
		p.advance()
		entityType, err := p.path()
		if err != nil {
			return Node{}, err
		}
		if p.peek().Text == "in" {
			p.advance()
			inEntity, err := p.add()
			if err != nil {
				return Node{}, err
			}
			return lhs.IsIn(entityType, inEntity), nil
		}
		return lhs.Is(entityType), nil
	}

	return lhs, err
}

func (p *parser) add() (Node, error) {
	lhs, err := p.mult()
	if err != nil {
		return Node{}, err
	}

	t := p.peek().Text
	operators := map[string]func(Node) Node{
		"+": lhs.Plus,
		"-": lhs.Minus,
	}
	if f, ok := operators[t]; ok {
		p.advance()
		rhs, err := p.mult()
		if err != nil {
			return Node{}, err
		}
		return f(rhs), nil
	}

	return lhs, nil
}

func (p *parser) mult() (Node, error) {
	lhs, err := p.unary()
	if err != nil {
		return Node{}, err
	}

	if p.peek().Text != "*" {
		return lhs, nil
	}

	p.advance()
	rhs, err := p.unary()
	if err != nil {
		return Node{}, err
	}
	return lhs.Times(rhs), nil
}

func (p *parser) unary() (Node, error) {
	var res Node
	var ops [](func(Node) Node)
	for {
		op := p.peek().Text
		switch op {
		case "!":
			p.advance()
			ops = append(ops, Not)
		case "-":
			p.advance()
			ops = append(ops, Negate)
		default:
			var err error
			res, err = p.member()
			if err != nil {
				return res, err
			}

			// TODO: add support for parsing -1 into a negative Long rather than a Negate(Long)
			for i := len(ops) - 1; i >= 0; i-- {
				res = ops[i](res)
			}
			return res, nil
		}
	}
}

func (p *parser) member() (Node, error) {
	res, err := p.primary()
	if err != nil {
		return res, err
	}
	for {
		var ok bool
		res, ok, err = p.access(res)
		if !ok {
			return res, err
		}
	}
}

func (p *parser) primary() (Node, error) {
	var res Node
	t := p.advance()
	switch {
	case t.isInt():
		i, err := t.intValue()
		if err != nil {
			return res, err
		}
		res = Long(types.Long(i))
	case t.isString():
		str, err := t.stringValue()
		if err != nil {
			return res, err
		}
		res = String(types.String(str))
	case t.Text == "true":
		res = True()
	case t.Text == "false":
		res = False()
	case t.Text == "principal":
		res = Principal()
	case t.Text == "action":
		res = Action()
	case t.Text == "resource":
		res = Resource()
	case t.Text == "context":
		res = Context()
	case t.isIdent():
		return p.entityOrExtFun(t.Text)
	case t.Text == "(":
		expr, err := p.expression()
		if err != nil {
			return res, err
		}
		if err := p.exact(")"); err != nil {
			return res, err
		}
		res = expr
	case t.Text == "[":
		set, err := p.expressions("]")
		if err != nil {
			return res, err
		}
		p.advance() // expressions guarantees "]"
		res = SetNodes(set...)
	case t.Text == "{":
		record, err := p.record()
		if err != nil {
			return res, err
		}
		res = record
	default:
		return res, p.errorf("invalid primary")
	}
	return res, nil
}

func (p *parser) entityOrExtFun(ident string) (Node, error) {
	// Technically, according to the grammar, both entities and extension functions
	// can have path prefixes and so parsing here is not trivial. In practice, there
	// are only two extension functions: `ip()` and `decimal()`, neither of which
	// have a path prefix. We'll just handle those two cases specially and treat
	// everything else as an entity.
	var res Node
	switch ident {
	case "ip", "decimal":
		if err := p.exact("("); err != nil {
			return res, err
		}
		t := p.advance()
		if !t.isString() {
			return res, p.errorf("expected string")
		}
		str, err := t.stringValue()
		if err != nil {
			return res, err
		}
		if err := p.exact(")"); err != nil {
			return res, err
		}

		if ident == "ip" {
			ipaddr, err := netip.ParsePrefix(str)
			if err != nil {
				return res, err
			}
			res = IPAddr(types.IPAddr(ipaddr))
		} else {
			dec, err := strconv.ParseFloat(str, 64)
			if err != nil {
				return res, err
			}
			res = Decimal(types.Decimal(dec))
		}
	default:
		entity, err := p.entityFirstPathPreread(ident)
		if err != nil {
			return res, err
		}
		res = Entity(entity)
	}

	return res, nil
}

func (p *parser) expressions(endOfListMarker string) ([]Node, error) {
	var res []Node
	for p.peek().Text != endOfListMarker {
		if len(res) > 0 {
			if err := p.exact(","); err != nil {
				return res, err
			}
		}
		e, err := p.expression()
		if err != nil {
			return res, err
		}
		res = append(res, e)
	}
	return res, nil
}

func (p *parser) record() (Node, error) {
	var res Node
	entries := map[types.String]Node{}
	for {
		t := p.peek()
		if t.Text == "}" {
			p.advance()
			return RecordNodes(entries), nil
		}
		if len(entries) > 0 {
			if err := p.exact(","); err != nil {
				return res, err
			}
		}
		k, v, err := p.recordEntry()
		if err != nil {
			return res, err
		}

		if _, ok := entries[k]; ok {
			return res, p.errorf("duplicate key: %v", k)
		}
		entries[k] = v
	}
}

func (p *parser) recordEntry() (types.String, Node, error) {
	var key types.String
	var value Node
	var err error
	t := p.advance()
	switch {
	case t.isIdent():
		key = types.String(t.Text)
	case t.isString():
		str, err := t.stringValue()
		if err != nil {
			return key, value, err
		}
		key = types.String(str)
	default:
		return key, value, p.errorf("unexpected token")
	}
	if err := p.exact(":"); err != nil {
		return key, value, err
	}
	value, err = p.expression()
	if err != nil {
		return key, value, err
	}
	return key, value, nil
}

func (p *parser) access(lhs Node) (Node, bool, error) {
	t := p.peek()
	switch t.Text {
	case ".":
		p.advance()
		t := p.advance()
		if !t.isIdent() {
			return Node{}, false, p.errorf("unexpected token")
		}
		if p.peek().Text == "(" {
			methodName := t.Text
			p.advance()
			exprs, err := p.expressions(")")
			if err != nil {
				return Node{}, false, err
			}
			p.advance() // expressions guarantees ")"

			knownMethods := map[string]func(Node) Node{
				"contains":    lhs.Contains,
				"containsAll": lhs.ContainsAll,
				"containsAny": lhs.ContainsAny,
			}
			if f, ok := knownMethods[methodName]; ok {
				if len(exprs) != 1 {
					return Node{}, false, p.errorf("%v expects one argument", methodName)
				}
				return f(exprs[0]), true, nil
			}
			return newExtMethodCallNode(lhs, types.String(methodName), exprs...), true, nil
		} else {
			return lhs.Access(t.Text), true, nil
		}
	case "[":
		p.advance()
		t := p.advance()
		if !t.isString() {
			return Node{}, false, p.errorf("unexpected token")
		}
		name, err := t.stringValue()
		if err != nil {
			return Node{}, false, err
		}
		if err := p.exact("]"); err != nil {
			return Node{}, false, err
		}
		return lhs.Access(name), true, nil
	default:
		return lhs, false, nil
	}
}
