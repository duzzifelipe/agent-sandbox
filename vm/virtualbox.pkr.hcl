packer {
  required_plugins {
    virtualbox = {
      source  = "github.com/hashicorp/virtualbox"
      version = "~> 1"
    }
  }
}

variable "vm_name" {
  type        = string
  description = "VM name; used as the OVA filename base"
}

variable "iso_url" {
  type        = string
  default     = "https://releases.ubuntu.com/24.04.2/ubuntu-24.04.2-live-server-amd64.iso"
  description = "URL or local path to the Ubuntu 24.04 server ISO"
}

variable "iso_checksum" {
  type        = string
  default     = "sha256:d6dab0c4cb67c685bf41e4585fd426b4b90cb0a8b5db7c90a9e5f84e8e1e1b0e"
  description = "sha256:<hex> checksum — verify at https://releases.ubuntu.com/24.04.2/SHA256SUMS"
}

variable "provision_script" {
  type        = string
  description = "Local path to the generated orchestration shell script"
}

variable "output_dir" {
  type        = string
  description = "Directory where the exported OVA will be written"
}

variable "ssh_password" {
  type      = string
  default   = "packer"
  sensitive = true
}

source "virtualbox-iso" "vm" {
  vm_name          = var.vm_name
  iso_url          = var.iso_url
  iso_checksum     = var.iso_checksum
  guest_os_type    = "Ubuntu_64"
  disk_size        = 20480
  memory           = 2048
  cpus             = 2
  headless         = true
  ssh_username     = "root"
  ssh_password     = var.ssh_password
  ssh_timeout      = "30m"
  shutdown_command = "shutdown -P now"

  http_content = {
    "/user-data" = file("${path.root}/autoinstall/user-data.yaml")
    "/meta-data" = ""
  }

  boot_wait = "5s"
  boot_command = [
    "c<wait>",
    "linux /casper/vmlinuz --- autoinstall ds=\"nocloud-net;s=http://{{.HTTPIP}}:{{.HTTPPort}}/\" <wait>",
    "<enter><wait>",
    "initrd /casper/initrd<wait>",
    "<enter><wait>",
    "boot<enter>"
  ]

  vboxmanage = [
    ["modifyvm", "{{.Name}}", "--nic1", "hostonly", "--hostonlyadapter1", "vboxnet0"],
  ]

  output_directory = var.output_dir
  format           = "ova"
}

build {
  sources = ["source.virtualbox-iso.vm"]

  provisioner "file" {
    source      = "${path.root}/"
    destination = "/tmp/agentsdx-vm/"
  }

  provisioner "shell" {
    script = var.provision_script
  }
}
