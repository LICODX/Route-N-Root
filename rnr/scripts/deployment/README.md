# RNR Blockchain - Deployment Scripts

Automated scripts untuk deploy RNR blockchain ke VPS Ubuntu.

## 📋 Prerequisites

- **Local machine**: Go 1.23+, SSH access ke VPS
- **VPS**: Ubuntu 20.04/22.04/24.04, minimal 2GB RAM, 20GB storage
- **SSH key** sudah di-setup untuk password-less login

## 🚀 Quick Start

### 1. Setup Single Node

```bash
# Setup VPS (install dependencies, firewall, systemd)
./scripts/deployment/setup-vps.sh 192.168.1.100 ubuntu

# Build & deploy binary
./scripts/deployment/build-and-deploy.sh 192.168.1.100 ubuntu

# Check health
./scripts/deployment/health-check.sh 192.168.1.100 ubuntu
```

### 2. Setup Multi-Node Network

```bash
# Setup 3 nodes in one command
./scripts/deployment/multi-node-setup.sh \
    192.168.1.100 \
    192.168.1.101 \
    192.168.1.102
```

## 📜 Scripts Overview

### `setup-vps.sh`
**Purpose**: Initial VPS configuration

**What it does:**
- ✅ Install Go 1.23
- ✅ Install dependencies (curl, wget, git)
- ✅ Configure UFW firewall (SSH port 22, P2P port 6000)
- ✅ Create directories
- ✅ Install systemd service

**Usage:**
```bash
./setup-vps.sh <vps-ip> [ssh-user]

# Examples:
./setup-vps.sh 192.168.1.100
./setup-vps.sh 192.168.1.100 ubuntu
```

---

### `build-and-deploy.sh`
**Purpose**: Build binary dan deploy ke VPS

**What it does:**
- ✅ Build optimized binary (CGO disabled, size reduced)
- ✅ Transfer via SCP
- ✅ Set executable permissions
- ✅ Restart systemd service
- ✅ Check status

**Usage:**
```bash
./build-and-deploy.sh <vps-ip> [ssh-user]

# Examples:
./build-and-deploy.sh 192.168.1.100
./build-and-deploy.sh 192.168.1.100 ubuntu
```

---

### `multi-node-setup.sh`
**Purpose**: Deploy multiple nodes sekaligus

**What it does:**
- ✅ Setup semua VPS secara parallel
- ✅ Build binary sekali saja
- ✅ Deploy ke semua nodes
- ✅ Start semua nodes
- ✅ Check status semua nodes

**Usage:**
```bash
./multi-node-setup.sh <node1-ip> <node2-ip> [node3-ip] ...

# Example: 5 nodes
./multi-node-setup.sh \
    192.168.1.100 \
    192.168.1.101 \
    192.168.1.102 \
    192.168.1.103 \
    192.168.1.104
```

---

### `health-check.sh`
**Purpose**: Check node health & status

**What it does:**
- ✅ Systemd service status
- ✅ Disk usage
- ✅ Memory usage
- ✅ Network ports
- ✅ Latest logs
- ✅ Firewall status
- ✅ Uptime

**Usage:**
```bash
./health-check.sh <vps-ip> [ssh-user]

# Examples:
./health-check.sh 192.168.1.100
./health-check.sh 192.168.1.100 ubuntu
```

## 🔧 Configuration

### Systemd Service
Service file installed at: `/etc/systemd/system/rnr-node.service`

**Useful commands:**
```bash
sudo systemctl start rnr-node      # Start
sudo systemctl stop rnr-node       # Stop
sudo systemctl restart rnr-node    # Restart
sudo systemctl status rnr-node     # Status
sudo systemctl enable rnr-node     # Auto-start on boot
sudo journalctl -u rnr-node -f     # View logs
```

### Firewall Rules
- Port 22: SSH (ALLOW)
- Port 6000: RNR P2P (ALLOW)

**Check firewall:**
```bash
sudo ufw status
```

## 📊 Monitoring

### View Real-time Logs
```bash
# Via script
./health-check.sh 192.168.1.100

# Direct SSH
ssh ubuntu@192.168.1.100 'sudo journalctl -u rnr-node -f'
```

### Check All Nodes
```bash
# Loop through all nodes
for IP in 192.168.1.{100..102}; do
    echo "=== Node $IP ==="
    ./health-check.sh $IP ubuntu
    echo ""
done
```

## 🔄 Update Deployment

```bash
# Update single node
./build-and-deploy.sh 192.168.1.100

# Update all nodes
for IP in 192.168.1.{100..104}; do
    ./build-and-deploy.sh $IP ubuntu &
done
wait
```

## 🐛 Troubleshooting

### Problem: Permission denied
```bash
# Fix SSH permissions
chmod 600 ~/.ssh/id_rsa
chmod 644 ~/.ssh/id_rsa.pub
```

### Problem: Service fails to start
```bash
# Check detailed logs
ssh ubuntu@192.168.1.100 'sudo journalctl -xeu rnr-node.service'

# Check binary permissions
ssh ubuntu@192.168.1.100 'ls -la /home/ubuntu/rnr-blockchain/rnr-node'
```

### Problem: Port already in use
```bash
# Check what's using port 6000
ssh ubuntu@192.168.1.100 'sudo netstat -tulpn | grep 6000'

# Kill process
ssh ubuntu@192.168.1.100 'sudo kill -9 <PID>'
```

### Problem: Firewall blocking
```bash
# Check UFW logs
ssh ubuntu@192.168.1.100 'sudo tail -f /var/log/ufw.log'

# Allow specific IP
ssh ubuntu@192.168.1.100 'sudo ufw allow from 192.168.1.101 to any port 6000'
```

## 📈 Production Checklist

Before mainnet launch:

- [ ] ✅ Test deployment on testnet VPS
- [ ] ✅ Verify all nodes can connect to each other
- [ ] ✅ Test automatic restart on crash
- [ ] ✅ Verify firewall rules
- [ ] ✅ Setup monitoring & alerting
- [ ] ✅ Configure database backups
- [ ] ✅ Document recovery procedures
- [ ] ✅ Deploy to at least 5 geographically distributed VPS

## 🔐 Security Best Practices

1. **SSH Key-based authentication only** (disable password login)
   ```bash
   sudo nano /etc/ssh/sshd_config
   # Set: PasswordAuthentication no
   sudo systemctl restart sshd
   ```

2. **Fail2ban for SSH protection**
   ```bash
   sudo apt install fail2ban
   sudo systemctl enable fail2ban
   ```

3. **Regular security updates**
   ```bash
   sudo apt update && sudo apt upgrade -y
   ```

4. **Limit sudo access** (only necessary users)

5. **Monitor logs regularly** for suspicious activity

## 📚 Resources

- [VPS Deployment Guide](../../docs/VPS_DEPLOYMENT.md)
- [Multi-Node Testing Guide](../../docs/MULTI_NODE_TESTING.md)

## 🆘 Support

Need help? Check logs first:
```bash
./health-check.sh <vps-ip>
```

Common issues usually solved by:
1. Restart service: `sudo systemctl restart rnr-node`
2. Check firewall: `sudo ufw status`
3. Verify binary exists: `ls -la ~/rnr-blockchain/rnr-node`
