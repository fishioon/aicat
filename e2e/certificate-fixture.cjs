const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
module.exports = (root, directory, expired = false) => {
  const generator = path.join(root, path.basename(directory) + "-root.go");
  fs.writeFileSync(
    generator,
    `package main
import ("crypto/rand"; "crypto/rsa"; "crypto/x509"; "crypto/x509/pkix"; "encoding/pem"; "math/big"; "os"; "path/filepath"; "time")
func main() {
 dir := os.Args[1]
 expired := os.Args[2] == "expired"
 name := "E2E TLS Root"
 before,after := time.Now().Add(-time.Hour),time.Now().Add(48*time.Hour)
 if expired { name = "Expired E2E Root"; before = time.Now().Add(-48*time.Hour); after = time.Now().Add(-24*time.Hour) }
 if err := os.MkdirAll(dir,0700); err != nil { panic(err) }
 key,err := rsa.GenerateKey(rand.Reader,2048); if err != nil { panic(err) }
 cert := &x509.Certificate{SerialNumber:big.NewInt(1), Subject:pkix.Name{CommonName:name}, NotBefore:before, NotAfter:after, DNSNames:[]string{"localhost","a.localhost","a.b.localhost"}, KeyUsage:x509.KeyUsageCertSign|x509.KeyUsageCRLSign|x509.KeyUsageDigitalSignature|x509.KeyUsageKeyEncipherment, BasicConstraintsValid:true, IsCA:true}
 der,err := x509.CreateCertificate(rand.Reader,cert,cert,&key.PublicKey,key); if err != nil { panic(err) }
 private,err := x509.MarshalPKCS8PrivateKey(key); if err != nil { panic(err) }
 public := pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:der})
 pair := append(pem.EncodeToMemory(&pem.Block{Type:"PRIVATE KEY",Bytes:private}), public...)
 if err := os.WriteFile(filepath.Join(dir,"mitmproxy-ca.pem"),pair,0600); err != nil { panic(err) }
 if err := os.WriteFile(filepath.Join(dir,"mitmproxy-ca-cert.pem"),public,0600); err != nil { panic(err) }
}
`,
  );
  fs.rmSync(directory, { recursive: true, force: true });
  execFileSync(
    "go",
    ["run", generator, directory, expired ? "expired" : "valid"],
    { timeout: 30000, stdio: ["ignore", "pipe", "pipe"] },
  );
  return {
    key: fs.readFileSync(path.join(directory, "mitmproxy-ca.pem")),
    cert: fs.readFileSync(path.join(directory, "mitmproxy-ca-cert.pem")),
    path: path.join(directory, "mitmproxy-ca-cert.pem"),
  };
};
