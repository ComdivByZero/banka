/* программа для обмена сообщениями по tcp; содержит серверный и клиентский режим*/
package main

import (
	"fmt"
	"flag"
	"net"
	"os/exec"
	"strings"
	"sync"
)

const (
	mpIntroduce		= 1
	mpClient		= 2
	mpSendMessage	= 3
	mpSendFile		= 4
	mpBye			= 5

	nickMinLen = 3
	nickMaxLen = 64
)

type (
	flags struct {
		help, exit bool
		port int
		nick string
		cmd struct {
			message string
		}
	}

	server struct {
		nick string
		ind int
		conns map[int] net.Conn
	}

	base struct {
		nick string
		servers map[string] *server
		serversMutex *sync.Mutex

		do bool
		listener net.Listener

		opts *flags
	}
)

func help() {
	fmt.Println(
		"	Банка - простейшая программа для обмена сообщениями по TCP из\n" + 
		"командной строки. Содержит серверный и клиентский режимы.\n" +
		"Для обмена сообщениями служат постоянно работающие сервера.\n" + 
		"Клиентское приложение отправляет сообщение запущенному на той же машине\n" + 
		"серверу, который, в свою очередь, перенаправляет его адресату.\n" +
		"При запуске сервера нужно перечислить список IP адресов собеседников.\n")
	flag.PrintDefaults()
}

func parse(f *flags) {
	const (
		sh = "запуск в серверном режиме"
		hh = "вывод справки"
		ph = "порт для работы с сервером"
		eh = "завершение работы сервера"
		mh = "команда, выполняемая при получении сообщения\n" +
			 "		   параметры: 1 - автор, 2 - сообщение"
	)
	flag.Usage = help

	flag.StringVar(&f.nick, "name", "", sh)
	flag.StringVar(&f.nick, "имя", "", sh)
	flag.BoolVar(&f.help, "help", false, hh)
	flag.BoolVar(&f.help, "справка", false, hh)
	flag.IntVar(&f.port, "port", 4725, ph)
	flag.IntVar(&f.port, "порт", 4725, ph)
	flag.BoolVar(&f.exit, "exit", false, eh)
	flag.BoolVar(&f.exit, "выход", false, eh)
	flag.StringVar(&f.cmd.message, "cmd-message", "", mh)
	flag.StringVar(&f.cmd.message, "кмд-сообщение", "", mh)

	flag.Parse()
}

func readStr(conn net.Conn) (string, error) {
	var (
		err error
		buf [65537]byte
		l int
		s string
	)
	_, err = conn.Read(buf[ : 2])
	s = ""
	if err == nil {
		l = int(buf[0]) + int(buf[1]) * 256
		_, err = conn.Read(buf[2 : l + 2])
		if err == nil {
			s = string(buf[2 : l + 2])
		}
	}
	return s, err
}

func sendName(conn net.Conn, nick string) error {
	var (
		buf [3] byte
		err error
	)
	buf[0] = mpIntroduce
	buf[1] = byte(len(nick) % 256)
	buf[2] = byte(len(nick) / 256 % 256)
	_, err = conn.Write(buf[:])
	if err == nil {
		_, err = conn.Write([]byte(nick))
	}
	return err
}

func getServer(b *base, conn net.Conn, nick string) *server {
	var (
		found bool
		ser *server
	)
	b.serversMutex.Lock()
	ser, found = b.servers[nick]
	if found {
		ser.conns[ser.ind] = conn
		ser.ind++
	} else {
		ser = new(server)
		ser.ind = 1
		ser.conns = make(map[int] net.Conn)
		ser.conns[0] = conn
		ser.nick = nick
		b.servers[nick] = ser
	}
	b.serversMutex.Unlock()
	return ser
}

func introduce(b *base, conn net.Conn) (*server, error) {
	var (
		buf [3] byte
		errNick error
		name [64] byte
		l int
		ser *server
		err error
	)
	ser = nil
	errNick = sendName(conn, b.nick)

	_, err = conn.Read(buf[ : 1])
	if err != nil || buf[0] == mpClient {

	} else if errNick != nil {
		err = errNick
	} else if buf[0] != mpIntroduce {
		err = fmt.Errorf("От сервера ожидался байт знакомства %d вместо %d", mpIntroduce, buf[0])
	} else {
		_, err = conn.Read(buf[1 : 3])
		if err == nil {
			l = int(buf[1]) + int(buf[2]) * 256
			if l < nickMinLen || l > nickMaxLen {
				err = fmt.Errorf("Сервер вернул неприемлемую длину имени = %d; %d <= l <= %d", l, nickMinLen, nickMaxLen)
			} else {
				_, err = conn.Read(name[ : l])
				if err == nil {
					ser = getServer(b, conn, string(name[ : l]))
				}
			}
		}
	}
	return ser, err
}

func handleClient(b *base, conn net.Conn) {
	var (
		err error
		buf [3 + 65536] byte
		far *server
		farFound bool
		name string
		l int
		con net.Conn
	)
	_, err = conn.Read(buf[ : 1])
	if err == nil {
		b.serversMutex.Lock()
		switch buf[0] {
			case mpBye:
				b.do = false
				for name, far = range b.servers {
					for _, con = range far.conns {
						con.Write(buf[ : 1])
						con.Close()
					}
				}
				b.listener.Close()
			case mpSendMessage:
				name, err = readStr(conn)
				if err == nil {
					far, farFound = b.servers[name]
					if !farFound {
						fmt.Println("Пользователь", name, "не подключён")
					} else {
						far = b.servers[name]
						_, err = conn.Read(buf[1 : 3])
						if err == nil {
							l = int(buf[1]) + int(buf[2]) * 256
							_, err = conn.Read(buf[3 : l + 3])
							if err == nil {
								for _, con = range far.conns {
									_, err = con.Write(buf[ : l + 3])
								}
							}
						}
					}
				}
			default:
				err = fmt.Errorf("Получена неизвестная команда от клиента : %d", buf[0])
		}
		b.serversMutex.Unlock()
	}
	if err != nil {
		fmt.Println(err)
	}
}

func handleServer(b *base, ser *server) {
	var (
		err error
		buf [1] byte
		s string
		con net.Conn
	)
	con = ser.conns[ser.ind - 1]
	fmt.Println("Подключился", ser.nick, ":", con.RemoteAddr())
	_, err = con.Read(buf[ : 1])
	for b.do && err == nil && buf[0] != mpBye {
		switch buf[0] {
			case mpSendMessage:
				s, err = readStr(con)
				if err == nil {
					fmt.Println(ser.nick, ":", s)
				}
				if b.opts.cmd.message != "" {
					exec.Command(b.opts.cmd.message, ser.nick, s).Start()
				}
			default:
				err = fmt.Errorf("%v послал неизвестную команду: %v", ser.nick, buf[0])
		}
		if err == nil {
			_, err = con.Read(buf[ : 1])
		}
	}
	con.Write([] byte {mpBye})
	delete(b.servers, ser.nick)
	if !b.do {

	} else if err != nil {
		fmt.Println(ser.nick, "отключился с ошибкой:", err)
	} else {
		fmt.Println(ser.nick, "отключился")
	}
}

func handleConnection(b *base, conn net.Conn) {
	var (
		err error
		ser *server
	)
	ser, err = introduce(b, conn)
	if err != nil {
		fmt.Println(err)
	} else if ser == nil {
		handleClient(b, conn)
	} else {
		handleServer(b, ser)
	}
	conn.Close()
}

func doServer(f *flags) error {
	var (
		err error
		conn net.Conn
		s string
		b base
	)
	if len(f.nick) < 3 {
		err = fmt.Errorf("Длина имени не должна быть меньше 3-х")
	} else {
		b.opts = f
		b.do = true
		b.nick = f.nick
		b.serversMutex = new(sync.Mutex)
		b.servers = make(map[string] *server)

		for _, s = range flag.Args() {
			if strings.IndexRune(s, ':') < 0 {
				s += ":4725"
			}
			conn, err = net.Dial("tcp", s)
			if err == nil {
				go handleConnection(&b, conn)
			}
		}
		b.listener, err = net.Listen("tcp", fmt.Sprint(":", f.port))
		if err == nil {
			conn, err = b.listener.Accept()
			for err == nil {
				go handleConnection(&b, conn)
				conn, err = b.listener.Accept()
			}
		}
	}
	return err
}

func writeStr(conn net.Conn, s string) (int, error) {
	var (
		dl, l int
		err error
	)
	l, err = conn.Write([] byte {byte(len(s) % 256), byte(len(s) / 256 % 256)})
	if err == nil {
		dl, err = conn.Write([] byte(s))
		l += dl
	}
	return l, err
}

func doClient(opts *flags) error {
	var (
		err error
		conn net.Conn
		buf = [...] byte {mpClient, mpSendMessage}
	)
	if !opts.exit && flag.NArg() < 2 {
		help()
		err = nil
	} else {
		conn, err = net.Dial("tcp", fmt.Sprint("127.0.0.1:", opts.port))
		if err != nil {
			err = fmt.Errorf("Клиенту не удалось подключиться к собственному локальному серверу:\n\t %v", err)
		} else {
			if opts.exit {
				buf[1] = mpBye
			}
			_, err = conn.Write(buf[ : ])
			if !opts.exit && err == nil {
				_, err = writeStr(conn, flag.Arg(0))
				if err == nil {
					_, err = writeStr(conn, flag.Arg(1))
				}
			}
			conn.Close()
		}
	}
	return err
}

func main() {
	var (
		opts flags
		err error
	)
	parse(&opts)
	if opts.help {
		help()
		err = nil
	} else if opts.nick != "" {
		err = doServer(&opts)
	} else {
		err = doClient(&opts)
	}
	if err != nil {
		fmt.Println(err)
	}
}
