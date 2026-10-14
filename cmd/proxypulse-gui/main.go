// Command proxypulse-gui is the native Windows desktop front-end for ProxyPulse.
package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/HVHBIGNAME/proxypulse/internal/store"
	"github.com/lxn/walk"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		message := err.Error()
		var nativeError *walk.Error
		if errors.As(err, &nativeError) {
			message = nativeError.Message()
		}
		walk.MsgBox(nil, "ProxyPulse", "Не удалось открыть приложение.\n\n"+message, walk.MsgBoxIconError)
		os.Exit(1)
	}
}

func run() (runErr error) {
	var logFile *os.File
	defer func() {
		if runErr != nil {
			log.Print(runErr)
		}
		if logFile != nil {
			log.SetOutput(os.Stderr)
			logFile.Close()
		}
	}()
	dir, err := appDir()
	if err != nil {
		return err
	}
	if lf, err := os.OpenFile(filepath.Join(dir, "proxypulse.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		logFile = lf
		log.SetOutput(lf)
	} else {
		log.Printf("Открытие журнала: %v", err)
	}
	db, err := store.Open(filepath.Join(dir, "proxies.json"))
	if err != nil {
		return fmt.Errorf("открытие кэша: %w", err)
	}
	defer db.Close()
	u := newDesktop(db, dir)
	if err := u.create(); err != nil {
		return err
	}
	defer u.dispose()
	icon, err := walk.NewIconFromResourceId(1)
	if err != nil {
		return fmt.Errorf("иконка приложения: %w; соберите GUI через scripts/build_release.py", err)
	}
	defer icon.Dispose()
	if err := u.mw.SetIcon(icon); err != nil {
		return err
	}
	u.loadCache()
	u.mw.Run()
	return nil
}

func appDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("папка данных: %w", err)
	}
	dir := filepath.Join(base, "ProxyPulse")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("создание папки данных: %w", err)
	}
	return dir, nil
}
