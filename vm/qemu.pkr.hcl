packer {
  required_plugins {
    qemu = {
      source  = "github.com/hashicorp/qemu"
      version = "~> 1"
    }
  }
}

variable "vm_name" {
  type        = string
  description = "VM name; used as the image filename base"
}

variable "iso_url" {
  type        = string
  description = "URL or local path to the Ubuntu server ISO — supplied by the caller (see server/internal/builder/builder.go isoRegistry)"
}

variable "iso_checksum" {
  type        = string
  description = "sha256:<hex> checksum for iso_url — supplied by the caller"
}

variable "provision_script" {
  type        = string
  description = "Local path to the generated orchestration shell script"
}

variable "output_dir" {
  type        = string
  description = "Directory where the built qcow2 image will be written"
}

variable "ssh_password" {
  type      = string
  default   = "packer"
  sensitive = true
}

variable "qemu_binary" {
  type        = string
  default     = "qemu-system-aarch64"
  description = "QEMU system binary; override to qemu-system-x86_64 on Intel"
}

variable "machine_type" {
  type    = string
  default = "virt"
}

variable "cpu_model" {
  type    = string
  default = "host"
}

variable "efi_firmware_code" {
  type        = string
  default     = "/opt/homebrew/share/qemu/edk2-aarch64-code.fd"
  description = "Path to EDK2 ARM64 firmware (Homebrew default); override for Intel"
}

source "qemu" "vm" {
  vm_name          = var.vm_name
  iso_url          = var.iso_url
  iso_checksum     = var.iso_checksum
  qemu_binary      = var.qemu_binary
  machine_type     = var.machine_type
  cpu_model        = var.cpu_model
  accelerator      = "hvf"
  disk_size        = "20480M"
  memory           = 2048
  cpus             = 2
  headless         = true
  ssh_username     = "root"
  ssh_password     = var.ssh_password
  ssh_timeout      = "30m"
  shutdown_command = "shutdown -P now"

  qemuargs = [
    ["-drive", "if=pflash,format=raw,readonly=on,file=${var.efi_firmware_code}"],
  ]

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

  output_directory = var.output_dir
  disk_image       = false
  format           = "qcow2"
}

build {
  sources = ["source.qemu.vm"]

  provisioner "file" {
    source      = "${path.root}/"
    destination = "/tmp/agentsdx-vm/"
  }

  provisioner "shell" {
    script = var.provision_script
  }
}
