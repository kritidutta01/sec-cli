class SecCli < Formula
  desc "Extract structured data from SEC EDGAR filings for LLM consumption"
  homepage "https://github.com/kritidutta01/sec-cli"
  version "1.0.1"

  on_macos do
    on_arm do
      url "https://github.com/kritidutta01/sec-cli/releases/download/v#{version}/sec-cli_#{version}_darwin_arm64.tar.gz"
      sha256 "9c510cf3b2d7a49c4ed567a786b4578f0e728092dc5235e2e60d6df9e1899221"
    end
    on_intel do
      url "https://github.com/kritidutta01/sec-cli/releases/download/v#{version}/sec-cli_#{version}_darwin_amd64.tar.gz"
      sha256 "9756bf547888a6bea7b90ab5da5e3e7bc84a4143b3cdb486d94578b379bf3781"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/kritidutta01/sec-cli/releases/download/v#{version}/sec-cli_#{version}_linux_arm64.tar.gz"
      sha256 "19aae224cb4eda139c6a23956e11cc0af68bdaa330cc6a2eeeee8b41525ec2d5"
    end
    on_intel do
      url "https://github.com/kritidutta01/sec-cli/releases/download/v#{version}/sec-cli_#{version}_linux_amd64.tar.gz"
      sha256 "76c1297714d295736a6878f79da171d58180db0f6830e5d5f91565cca8fd69e3"
    end
  end

  def install
    bin.install "sec-cli"
  end

  test do
    # The binary exits 0 for version; anything else means the binary is broken.
    assert_match version.to_s, shell_output("#{bin}/sec-cli version")
  end
end
