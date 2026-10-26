package examiner

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/Pastranauwu/loom/internal/loop"
)

const usoArchivo = "examinador-usage.jsonl"

func guardarUso(root, id string, uso loop.Tokens) error {
	p := filepath.Join(loop.RunsDir(root), id, usoArchivo)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(uso)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

// LeerUso devuelve cada invocación del examinador, incluidas las fallidas.
func LeerUso(root, id string) ([]loop.Tokens, error) {
	p := filepath.Join(loop.RunsDir(root), id, usoArchivo)
	f, err := os.Open(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []loop.Tokens
	s := bufio.NewScanner(f)
	for s.Scan() {
		var u loop.Tokens
		if err := json.Unmarshal(s.Bytes(), &u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, s.Err()
}
