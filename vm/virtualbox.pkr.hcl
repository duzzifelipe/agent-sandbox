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
  description = "URL or local path to the Ubuntu server ISO — must be supplied by the caller (see server/internal/builder/builder.go isoRegistry)"
}

variable "iso_checksum" {
  type        = string
  description = "sha256:<hex> checksum for iso_url — must be supplied by the caller (see server/internal/builder/builder.go isoRegistry)"
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
