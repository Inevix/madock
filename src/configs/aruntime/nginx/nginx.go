package nginx

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/faradey/madock/src/configs"
	"github.com/faradey/madock/src/paths"
)

func MakeConf() {
	paths.MakeDirsByPath(paths.GetExecDirPath() + "/projects/" + paths.GetRunDirName() + "/docker/nginx")
	setPorts()
	makeProxy()
	makeDockerfile()
	makeDockerCompose()
}

func setPorts() {
	projectsAruntime := paths.GetDirs(paths.MakeDirsByPath(paths.GetExecDirPath() + "/aruntime/projects"))
	projects := paths.GetDirs(paths.MakeDirsByPath(paths.GetExecDirPath() + "/projects"))
	if len(projectsAruntime) > len(projects) {
		projects = projectsAruntime
	}
	portsFile := paths.GetExecDirPath() + "/aruntime/ports.conf"
	if _, err := os.Stat(portsFile); os.IsNotExist(err) {
		lines := ""
		for port, line := range projects {
			lines += line + "=" + strconv.Itoa(port+1) + "\n"
		}
		_ = ioutil.WriteFile(portsFile, []byte(lines), 0664)
	}

	portsConfig := configs.ParseFile(portsFile)

	if _, ok := portsConfig[paths.GetRunDirName()]; !ok {
		f, err := os.OpenFile(portsFile,
			os.O_APPEND|os.O_WRONLY, 0664)
		if err != nil {
			log.Println(err)
		}
		defer f.Close()
		maxPort := getMaxPort(portsConfig)
		if _, err := f.WriteString(paths.GetRunDirName() + "=" + strconv.Itoa(maxPort+1) + "\n"); err != nil {
			log.Println(err)
		}
	}
}

func makeProxy() {
	portsFile := paths.GetExecDirPath() + "/aruntime/ports.conf"
	portsConfig := configs.ParseFile(portsFile)
	/* Create nginx default configuration for Magento2 */
	nginxDefFile := paths.GetExecDirPath() + "/docker/nginx/conf/default-proxy.conf"
	allFileData := "worker_processes 2;\nworker_priority -10;\nworker_rlimit_nofile 200000;\nevents {\n    worker_connections 4096;\nuse epoll;\n}\nhttp {\n"
	b, err := os.ReadFile(nginxDefFile)
	if err != nil {
		log.Fatal(err)
	}

	str := string(b)
	projectsNames := paths.GetDirs(paths.GetExecDirPath() + "/aruntime/projects")
	for _, name := range projectsNames {
		if _, err := os.Stat(paths.GetExecDirPath() + "/projects/" + name + "/env.txt"); !os.IsNotExist(err) {
			if _, err = os.Stat(paths.GetExecDirPath() + "/aruntime/projects/" + name + "/stopped"); os.IsNotExist(err) {
				port, err := strconv.Atoi(portsConfig[name])
				if err != nil {
					log.Fatal(err)
				}
				portRanged := (port - 1) * 20
				strReplaced := strings.Replace(str, "{{{NGINX_PORT}}}", strconv.Itoa(17000+portRanged), -1)
				for i := 1; i < 20; i++ {
					strReplaced = strings.Replace(strReplaced, "{{{NGINX_PORT+"+strconv.Itoa(i)+"}}}", strconv.Itoa(17000+portRanged+i), -1)
				}
				hostName := "loc." + name + ".com"
				projectConf := configs.GetProjectConfig(name)
				if val, ok := projectConf["HOSTS"]; ok {
					var onlyHosts []string
					hosts := strings.Split(val, " ")
					if len(hosts) > 0 {
						for _, hostAndStore := range hosts {
							onlyHosts = append(onlyHosts, strings.Split(hostAndStore, ":")[0])
						}
						hostName = strings.Join(onlyHosts, "\n")
					}
				}

				strReplaced = strings.Replace(strReplaced, "{{{HOST_NAMES}}}", hostName, -1)
				allFileData += "\n" + strReplaced
			}
		}
	}
	allFileData += "\n}"
	nginxFile := paths.MakeDirsByPath(paths.GetExecDirPath()+"/aruntime/ctx") + "/proxy.conf"
	err = ioutil.WriteFile(nginxFile, []byte(allFileData), 0755)
	if err != nil {
		log.Fatalf("Unable to write file: %v", err)
	}
	/* END Create nginx default configuration for Magento2 */
}

func makeDockerfile() {
	/* Create nginx Dockerfile configuration */
	ctxPath := paths.MakeDirsByPath(paths.GetExecDirPath() + "/aruntime/ctx")
	nginxDefFile := paths.GetExecDirPath() + "/docker/nginx/proxy.Dockerfile"
	b, err := os.ReadFile(nginxDefFile)
	if err != nil {
		log.Fatal(err)
	}

	err = ioutil.WriteFile(ctxPath+"/Dockerfile", b, 0755)
	if err != nil {
		log.Fatalf("Unable to write file: %v", err)
	}

	GenerateSslCert(ctxPath, false)
	/* END Create nginx Dockerfile configuration */
}

func makeDockerCompose() {
	/* Copy nginx docker-compose configuration */
	paths.MakeDirsByPath(paths.GetExecDirPath() + "/aruntime/ctx")
	nginxDefFile := paths.GetExecDirPath() + "/docker/nginx/docker-compose-proxy.yml"
	b, err := os.ReadFile(nginxDefFile)
	if err != nil {
		log.Fatal(err)
	}

	err = ioutil.WriteFile(paths.GetExecDirPath()+"/aruntime/docker-compose.yml", b, 0755)
	if err != nil {
		log.Fatalf("Unable to write file: %v", err)
	}
	/* END Create nginx Dockerfile configuration */
}

func getMaxPort(conf map[string]string) int {
	max := 0
	portInt := 0
	var err error
	for _, port := range conf {
		portInt, err = strconv.Atoi(port)
		if err != nil {
			log.Fatal(err)
		}
		if max < portInt {
			max = portInt
		}
	}

	return max
}

func GenerateSslCert(ctxPath string, force bool) {
	generalConfig := configs.GetGeneralConfig()
	if val, ok := generalConfig["SSL"]; force || (ok && val == "true") {
		projectsNames := paths.GetDirs(paths.GetExecDirPath() + "/aruntime/projects")
		var commands []string
		var i int = 0
		for _, name := range projectsNames {
			projectConf := configs.GetProjectConfig(name)
			if val, ok := projectConf["HOSTS"]; ok {
				var onlyHost string
				hosts := strings.Split(val, " ")
				if len(hosts) > 0 {
					for _, hostAndStore := range hosts {
						onlyHost = strings.Split(hostAndStore, ":")[0]
						commands = append(commands, "DNS."+strconv.Itoa(i+2)+" = "+onlyHost)
						i++
					}
				}
			}
		}

		extFileContent := "authorityKeyIdentifier=keyid,issuer\n" +
			"basicConstraints=CA:FALSE\n" +
			"keyUsage = digitalSignature, nonRepudiation, keyEncipherment, dataEncipherment\n" +
			"subjectAltName = @alt_names\n" +
			"\n" +
			"[alt_names]\n" +
			"DNS.1 = madocklocalkey\n" +
			strings.Join(commands, "\n")

		err := ioutil.WriteFile(ctxPath+"/madock.ca.ext", []byte(extFileContent), 0755)
		if err != nil {
			log.Fatalf("Unable to write file: %v", err)
		}

		sslConfigFileContent := "ssl_session_cache shared:le_nginx_SSL:1m;\n" +
			"ssl_session_timeout 1440m;\n" +
			"\n" +
			"ssl_protocols TLSv1 TLSv1.1 TLSv1.2;\n" +
			"ssl_prefer_server_ciphers on;\n" +
			"\n" +
			"ssl_ciphers \"ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-AES128-SHA256:ECDHE-RSA-AES128-SHA256:ECDHE-ECDSA-AES128-SHA:ECDHE-RSA-AES256-SHA384:ECDHE-RSA-AES128-SHA:ECDHE-ECDSA-AES256-SHA384:ECDHE-ECDSA-AES256-SHA:ECDHE-RSA-AES256-SHA:DHE-RSA-AES128-SHA256:DHE-RSA-AES128-SHA:DHE-RSA-AES256-SHA256:DHE-RSA-AES256-SHA:ECDHE-ECDSA-DES-CBC3-SHA:ECDHE-RSA-DES-CBC3-SHA:EDH-RSA-DES-CBC3-SHA:AES128-GCM-SHA256:AES256-GCM-SHA384:AES128-SHA256:AES256-SHA256:AES128-SHA:AES256-SHA:DES-CBC3-SHA:!DSS\";"

		err = ioutil.WriteFile(ctxPath+"/options-ssl-nginx.conf", []byte(sslConfigFileContent), 0755)
		if err != nil {
			log.Fatalf("Unable to write file: %v", err)
		}

		doGenerateSsl := false
		if _, err := os.Stat(ctxPath + "/madockCA.pem"); os.IsNotExist(err) {
			doGenerateSsl = true
		} else {
			certificateCreatedTime, err := os.Stat(ctxPath + "/madockCA.pem")
			if err == nil && certificateCreatedTime.ModTime().Unix() < time.Now().Unix()-363*86400 {
				doGenerateSsl = true
			}
		}

		if doGenerateSsl || force {
			cmd := exec.Command("openssl", "req", "-x509", "-newkey", "rsa:4096", "-keyout", ctxPath+"/madockCA.key", "-out", ctxPath+"/madockCA.pem", "-sha256", "-days", "365", "-nodes", "-subj", "/CN=madock")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			err = cmd.Run()
			if err != nil {
				log.Fatal(err)
			}

			fmt.Println("Enter your password for adding an SSL certificate to your system.")

			if runtime.GOOS == "darwin" {
				cmd = exec.Command("sudo", "security", "delete-certificate", "-c", "madock")
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				err = cmd.Run()
				if err != nil {
					log.Fatal(err)
				}

				cmd = exec.Command("sudo", "security", "add-trusted-cert", "-d", "-r", "trustRoot", "-k", "/Library/Keychains/System.keychain", ctxPath+"/madockCA.pem")
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				err = cmd.Run()
				if err != nil {
					log.Fatal(err)
				}
			} else if runtime.GOOS == "linux" {
				cmd = exec.Command("sudo", "cp", ctxPath+"/madockCA.pem", "/usr/local/share/ca-certificates/madockCA.crt")
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				err = cmd.Run()
				if err != nil {
					log.Fatal(err)
				}

				cmd = exec.Command("sudo", "chmod", "644", "/usr/local/share/ca-certificates/madockCA.crt")
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				err = cmd.Run()
				if err != nil {
					log.Fatal(err)
				}

				cmd = exec.Command("certutil", "-H")
				var outb, errb bytes.Buffer
				cmd.Stdout = &outb
				cmd.Stderr = &errb
				err = cmd.Run()
				selected := "y"
				if err != nil && errb.String() == "" {
					fmt.Println("You need to install \"certutil\" to proceed with the certificate installation. Continue installation? y - continue. n - cancel certificate generation and continue without ssl.")
					fmt.Print("> ")
					buf := bufio.NewReader(os.Stdin)
					sentence, err := buf.ReadBytes('\n')
					if err != nil {
						log.Fatalln(err)
					}
					selected = strings.TrimSpace(string(sentence))
					if selected == "y" {
						cmd = exec.Command("sudo", "apt", "install", "-y", "libnss3-tools")
						cmd.Stdout = os.Stdout
						cmd.Stderr = os.Stderr
						err = cmd.Run()
						if err != nil {
							log.Fatal(err)
						}
					}
				}

				if selected == "y" {
					usr, _ := user.Current()
					cmd = exec.Command("certutil", "-d", "sql:"+usr.HomeDir+"/.pki/nssdb", "-A", "-t", "C,,", "-n", "madocklocalkey", "-i", ctxPath+"/madockCA.pem")
					cmd.Stdout = os.Stdout
					cmd.Stderr = os.Stderr
					err = cmd.Run()
					if err != nil {
						log.Fatal(err)
					}
				}

				cmd = exec.Command("sudo", "update-ca-certificates", "-f")
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				err = cmd.Run()
				if err != nil {
					log.Fatal(err)
				}
			}
		}

		cmd := exec.Command("openssl", "req", "-newkey", "rsa:4096", "-keyout", ctxPath+"/madock.local.key", "-out", ctxPath+"/madock.local.csr", "-nodes", "-subj", "/CN=madocklocalkey")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			log.Fatal(err)
		}

		cmd = exec.Command("openssl", "x509", "-req", "-in", ctxPath+"/madock.local.csr", "-CA", ctxPath+"/madockCA.pem", "-CAkey", ctxPath+"/madockCA.key", "-CAcreateserial", "-out", ctxPath+"/madock.local.crt", "-days", "365", "-sha256", "-extfile", ctxPath+"/madock.ca.ext")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			log.Fatal(err)
		}

		cmd = exec.Command("bash", "-c", "cat "+ctxPath+"/madock.local.crt "+ctxPath+"/madockCA.pem > "+ctxPath+"/fullchain.crt")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			log.Fatal(err)
		}
	}
}
