# Bootstrap a pristine Redmine container for e2e tests. Idempotent.
# Run via: bin/rails runner /bootstrap.rb  (inside the container)
# Prints two prefixed lines: e2e_token=<api token>, e2e_project=<identifier>.
# The prefixes let run.sh find these lines even if rails runner also emits
# warnings to stdout (e.g. Redmine 5.1's "Creating scope" ActiveRecord
# warnings).
begin
  Redmine::DefaultData::Loader.load('en')
rescue Redmine::DefaultData::DataAlreadyLoaded
end

Setting.rest_api_enabled = '1'

admin = User.find_by_login('admin')
admin.update_columns(must_change_passwd: false, status: User::STATUS_ACTIVE)

token = Token.find_by(user_id: admin.id, action: 'api') ||
        Token.create!(user: admin, action: 'api')

project = Project.find_by_identifier('e2e') ||
          Project.create!(name: 'E2E', identifier: 'e2e', is_public: false)
project.enabled_module_names = (project.enabled_module_names + %w[issue_tracking time_tracking wiki]).uniq
project.trackers = Tracker.sorted.to_a if project.trackers.empty?
project.save!

IssueCategory.find_or_create_by!(project: project, name: 'E2E Cat')

unless IssueCustomField.find_by(name: 'E2E Text')
  IssueCustomField.create!(name: 'E2E Text', field_format: 'string',
                           is_for_all: true, trackers: Tracker.sorted.to_a)
end

puts "e2e_token=#{token.value}"
puts "e2e_project=#{project.identifier}"
