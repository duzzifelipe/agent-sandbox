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
  description = "VM name; used as the output filename base"
}

variable "iso_url" {
  type        = string
  description = "URL or local path to the Ubuntu ARM64 server ISO"
}

variable "iso_checksum" {
  type        = string
  description = "sha256:<hex> checksum for iso_url"
}

variable "provision_script" {
  type        = string
  description = "Local path to the generated orchestration shell script"
}

variable "output_dir" {
  type        = string
  description = "Directory where the exported raw disk image will be written"
}

variable "ssh_password" {
  type      = string
  default   = "packer"
  sensitive = true
}

source "qemu" "applevz" {
  vm_name          = "${var.vm_name}.img"
  iso_url          = var.iso_url
  iso_checksum     = var.iso_checksum
  output_directory = var.output_dir
  disk_size        = "20480M"
  memory           = 4096
  cpus             = 4
  headless         = true
  format           = "raw"
  accelerator      = "hvf"
  machine_type     = "virt"
  qemu_binary      = "qemu-system-aarch64"

  # edk2-aarch64-code.fd is shipped with QEMU installed via Homebrew.
  # If installed via another package manager, adjust the path accordingly.
  firmware = "/opt/homebrew/share/qemu/edk2-aarch64-code.fd"

  ssh_username     = "root"
  ssh_password     = var.ssh_password
  ssh_timeout      = "45m"
  shutdown_command = "shutdown -P now"

  http_content = {
    "/user-data" = file("${path.root}/autoinstall/user-data.yaml")
    "/meta-data" = ""
  }

  # Boot command for Ubuntu 24.04 ARM64 with UEFI/GRUB.
  # Press 'e' to edit the first GRUB entry, navigate to the linux line,
  # append autoinstall parameters, then boot with F10.
  boot_wait = "20s"
  boot_command = [
    "e<wait5>",
    "<down><down><down><end>",
    " autoinstall ds=\"nocloud-net;s=http://{{.HTTPIP}}:{{.HTTPPort}}/\"<wait>",
    "<f10>"
  ]

  qemuargs = [
    ["-device", "virtio-gpu-pci"],
    ["-device", "usb-ehci,id=usb,bus=pcie.0"],
    ["-device", "usb-kbd,bus=usb.0"],
    ["-device", "usb-mouse,bus=usb.0"],
  ]
}

build {
  sources = ["source.qemu.applevz"]

  provisioner "file" {
    source      = "${path.root}/"
    destination = "/tmp/agentsdx-vm/"
  }

  provisioner "shell" {
    script = var.provision_script
  }
}
