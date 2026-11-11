package shellenv

import "github.com/zhYou/my/internal/base/agecrypt"

func (s *Service) ageKey() (*agecrypt.Key, error) {
	if s.options.AgeKey != nil {
		return s.options.AgeKey, nil
	}
	if s.options.LoadAgeKey != nil {
		key, err := s.options.LoadAgeKey()
		if err != nil {
			return nil, problem("shellenv.identity", phaseReadIdentity, err)
		}
		if key != nil {
			return key, nil
		}
	}
	return nil, problem("shellenv.identity", phaseReadIdentity, nil)
}

const phaseReadIdentity = "shellenv.identity"
